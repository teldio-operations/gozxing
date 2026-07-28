package oned

import (
	"math"
	"sort"

	"github.com/teldio-operations/gozxing"
)

type RowDecoder interface {
	// decodeRow Attempts to decode a one-dimensional barcode format given a single row of an image
	// @param rowNumber row number from top of the row
	// @param row the black/white pixel data of the row
	// @param hints decode hints
	// @return {@link Result} containing encoded string and start/end of barcode
	// @throws NotFoundException if no potential barcode is found
	// @throws ChecksumException if a potential barcode is found but does not pass its checksum
	// @throws FormatException if a potential barcode is found but format is invalid
	DecodeRow(rowNumber int, row *gozxing.BitArray, hints map[gozxing.DecodeHintType]interface{}) (*gozxing.Result, error)
}

// OneDReader Encapsulates functionality and implementation that is common to all families
// of one-dimensional barcodes.
type OneDReader struct {
	RowDecoder
}

func NewOneDReader(rowDecoder RowDecoder) *OneDReader {
	return &OneDReader{rowDecoder}
}

func (this *OneDReader) DecodeWithoutHints(image *gozxing.BinaryBitmap) ([]*gozxing.Result, error) {
	return this.Decode(image, nil)
}

// Decode Note that we don't try rotation without the try harder flag, even if rotation was supported.
func (this *OneDReader) Decode(
	image *gozxing.BinaryBitmap, hints map[gozxing.DecodeHintType]interface{}) ([]*gozxing.Result, error) {

	results, e := this.doDecode(image, hints)
	if e == nil {
		return results, nil
	}

	if _, ok := e.(gozxing.NotFoundException); !ok {
		return nil, e
	}

	_, tryHarder := hints[gozxing.DecodeHintType_TRY_HARDER]
	if !(tryHarder && image.IsRotateSupported()) {
		return nil, e
	}

	rotatedImage, e := image.RotateCounterClockwise()
	if e != nil {
		return nil, gozxing.WrapReaderException(e)
	}

	results, e = this.doDecode(rotatedImage, hints)
	if e != nil {
		return nil, e
	}
	height := float64(rotatedImage.GetHeight())
	for _, result := range results {
		// Record that we found it rotated 90 degrees CCW / 270 degrees CW
		metadata := result.GetResultMetadata()
		orientation := 270
		if o, ok := metadata[gozxing.ResultMetadataType_ORIENTATION]; ok {
			// But if we found it reversed in doDecode(), add in that result here:
			orientation = (orientation + o.(int)) % 360
		}
		result.PutMetadata(gozxing.ResultMetadataType_ORIENTATION, orientation)
		// Update result points
		points := result.GetResultPoints()
		for i := 0; i < len(points); i++ {
			points[i] = gozxing.NewResultPoint(height-points[i].GetY()-1, points[i].GetX())
		}
	}
	return results, nil
}

func (this *OneDReader) Reset() {
	// do nothing
}

// resultKey identifies one decoded barcode. Every scanned row that lies inside a barcode
// decodes to the same format and the same text, so those rows share one key.
type resultKey struct {
	format gozxing.BarcodeFormat
	text   string
}

// rowResult is a decoded barcode and the row it first came from. doDecode sorts on the row to
// report the barcodes down the image, whatever order the scan found them in.
type rowResult struct {
	row    int
	result *gozxing.Result
}

// sweepRowStep bounds the gap between the rows of the full-image sweep. The gap is a
// fraction of the image height, which suits an image of one barcode. An image of stacked
// barcodes is much taller, and that fraction would step over the short ones.
const sweepRowStep = 8

// scanRows lists the rows to scan, in the order to scan them.
//
// The first rows are the center-out sweep that a single-barcode image needs: the center of such
// an image is the most reliable place to read, and rows farther out are progressively worse.
// The rows after that sweep the whole image, which is what finds the barcodes of a stack.
func scanRows(height int, tryHarder bool) []int {
	rowStep := max(1, height>>5)
	maxLines := 15 // 15 rows spaced 1/32 apart is roughly the middle half of the image
	if tryHarder {
		rowStep = max(1, height>>8)
		maxLines = height // Look at the whole image, not just the center
	}

	rows := make([]int, 0, maxLines)
	queued := make([]bool, height)
	enqueue := func(rowNumber int) {
		if !queued[rowNumber] {
			queued[rowNumber] = true
			rows = append(rows, rowNumber)
		}
	}

	// We're going to examine rows from the middle outward, searching alternately above and below
	// the middle, and farther out each time. rowStep is the number of rows between each
	// successive attempt above and below the middle. So we'd scan row middle, then
	// middle - rowStep, then middle + rowStep, then middle - (2 * rowStep), etc.
	middle := height / 2
	for x := 0; x < maxLines; x++ {
		// Scanning from the middle out. Determine which row we're looking at next:
		rowStepsAboveOrBelow := (x + 1) / 2
		isAbove := (x & 0x01) == 0 // i.e. is x even?
		rowNumber := middle
		if isAbove {
			rowNumber += rowStep * rowStepsAboveOrBelow
		} else {
			rowNumber -= rowStep * rowStepsAboveOrBelow
		}
		if rowNumber < 0 || rowNumber >= height {
			// Oops, if we run off the top or bottom, stop
			break
		}
		enqueue(rowNumber)
	}

	for rowNumber := 0; rowNumber < height; rowNumber += min(rowStep, sweepRowStep) {
		enqueue(rowNumber)
	}
	return rows
}

// doDecode scans the rows that scanRows lists and collects every barcode they decode to.
//
// @param image The image to decode
// @param hints Any hints that were requested
// @return one result per barcode, ordered down the image
// @throws NotFoundException if the image holds no barcode
func (this *OneDReader) doDecode(
	image *gozxing.BinaryBitmap, hints map[gozxing.DecodeHintType]interface{}) ([]*gozxing.Result, error) {

	width := image.GetWidth()
	height := image.GetHeight()
	row := gozxing.NewBitArray(width)
	adjacentRow := gozxing.NewBitArray(width)

	_, tryHarder := hints[gozxing.DecodeHintType_TRY_HARDER]

	rowResults := make([]rowResult, 0, 1)
	found := make(map[resultKey]bool)

	for _, rowNumber := range scanRows(height, tryHarder) {

		// Estimate black point for this row and load it:
		row, e := image.GetBlackRow(rowNumber, row)
		if e != nil {
			if _, ok := e.(gozxing.NotFoundException); ok {
				continue
			}
			return nil, gozxing.WrapReaderException(e)
		}

		// While we have the image data in a BitArray, it's fairly cheap to reverse it in place to
		// handle decoding upside down barcodes.
		for attempt := 0; attempt < 2; attempt++ {
			if attempt == 1 { // trying again?
				row.Reverse() // reverse the row and continue
				// This means we will only ever draw result points *once* in the life of this method
				// since we want to avoid drawing the wrong points after flipping the row, and,
				// don't want to clutter with noise from every single row scan -- just the scans
				// that start on the center line.
				if _, ok := hints[gozxing.DecodeHintType_NEED_RESULT_POINT_CALLBACK]; ok {
					newHints := make(map[gozxing.DecodeHintType]interface{})
					for k, v := range hints {
						if k != gozxing.DecodeHintType_NEED_RESULT_POINT_CALLBACK {
							newHints[k] = v
						}
					}
					hints = newHints
				}
			}

			// Look for a barcode
			result, e := this.DecodeRow(rowNumber, row, hints)
			if e != nil {
				if _, ok := e.(gozxing.ReaderException); !ok {
					return nil, e
				}
				continue // just couldn't decode this row
			}

			if attempt == 1 {
				// We found our barcode, but it was upside down, so note that
				result.PutMetadata(gozxing.ResultMetadataType_ORIENTATION, 180)
				// And remember to flip the result points horizontally.
				points := result.GetResultPoints()
				if len(points) >= 2 {
					w := float64(width)
					points[0] = gozxing.NewResultPoint(w-points[0].GetX()-1, points[0].GetY())
					points[1] = gozxing.NewResultPoint(w-points[1].GetX()-1, points[1].GetY())
				}
			}

			// The first barcode found is the one the reader would have returned before it could
			// return several, so take it as it is. Every barcode after it has to repeat on an
			// adjacent row, which is what keeps a lucky misread of a noisy row out of the results.
			key := resultKey{result.GetBarcodeFormat(), result.GetText()}
			if !found[key] {
				if len(rowResults) == 0 ||
					this.repeatsOnAdjacentRow(image, rowNumber, attempt == 1, key, hints, adjacentRow) {
					found[key] = true
					rowResults = append(rowResults, rowResult{rowNumber, result})
				}
			}
			// The reversed row holds the same barcode, so stop here and move to the next row.
			break
		}
	}

	if len(rowResults) == 0 {
		return nil, gozxing.NewNotFoundException()
	}

	sort.SliceStable(rowResults, func(i, j int) bool { return rowResults[i].row < rowResults[j].row })
	results := make([]*gozxing.Result, len(rowResults))
	for i, rr := range rowResults {
		results[i] = rr.result
	}
	return results, nil
}

// repeatsOnAdjacentRow reads the row above and the row below rowNumber, and reports whether
// either one holds the same barcode.
//
// A barcode is many pixels tall, so a row next to it reads the same. A blurred or noisy row
// sometimes decodes to something that passes its checksum by luck, and that misread does not
// repeat on the row beside it.
func (this *OneDReader) repeatsOnAdjacentRow(image *gozxing.BinaryBitmap, rowNumber int, reversed bool,
	key resultKey, hints map[gozxing.DecodeHintType]interface{}, row *gozxing.BitArray) bool {

	height := image.GetHeight()
	for _, neighbor := range [2]int{rowNumber + 1, rowNumber - 1} {
		if neighbor < 0 || neighbor >= height {
			continue
		}
		row, e := image.GetBlackRow(neighbor, row)
		if e != nil {
			continue
		}
		if reversed {
			row.Reverse()
		}
		result, e := this.DecodeRow(neighbor, row, hints)
		if e != nil {
			continue
		}
		if (resultKey{result.GetBarcodeFormat(), result.GetText()}) == key {
			return true
		}
	}
	return false
}

// RecordPattern Records the size of successive runs of white and black pixels in a row,
// starting at a given point.
// The values are recorded in the given array, and the number of runs recorded is equal to the size
// of the array. If the row starts on a white pixel at the given start point, then the first count
// recorded is the run of white pixels starting from that point; likewise it is the count of a run
// of black pixels if the row begin on a black pixels at that point.
//
// @param row row to count from
// @param start offset into row to start at
// @param counters array into which to record counts
// @throws NotFoundException if counters cannot be filled entirely from row before running out
//
//	of pixels
func RecordPattern(row *gozxing.BitArray, start int, counters []int) error {
	numCounters := len(counters)
	for i := range counters {
		counters[i] = 0
	}
	end := row.GetSize()
	if start >= end {
		return gozxing.NewNotFoundException("start=%v, end=%v", start, end)
	}
	isWhite := !row.Get(start)
	counterPosition := 0
	i := start
	for i < end {
		if row.Get(i) != isWhite {
			counters[counterPosition]++
		} else {
			counterPosition++
			if counterPosition == numCounters {
				break
			} else {
				counters[counterPosition] = 1
				isWhite = !isWhite
			}
		}
		i++
	}
	// If we read fully the last section of pixels and filled up our counters -- or filled
	// the last counter but ran off the side of the image, OK. Otherwise, a problem.
	if !(counterPosition == numCounters || (counterPosition == numCounters-1 && i == end)) {
		return gozxing.NewNotFoundException()
	}
	return nil
}

func RecordPatternInReverse(row *gozxing.BitArray, start int, counters []int) error {
	// This could be more efficient I guess
	numTransitionsLeft := len(counters)
	last := row.Get(start)
	for start > 0 && numTransitionsLeft >= 0 {
		start--
		if row.Get(start) != last {
			numTransitionsLeft--
			last = !last
		}
	}
	if numTransitionsLeft >= 0 {
		return gozxing.NewNotFoundException("numTransitionsLeft = %v", numTransitionsLeft)
	}
	return RecordPattern(row, start+1, counters)
}

// PatternMatchVariance Determines how closely a set of observed counts of runs of
// black/white values matches a given target pattern.
// This is reported as the ratio of the total variance from the expected pattern
// proportions across all pattern elements, to the length of the pattern.
//
// @param counters observed counters
// @param pattern expected pattern
// @param maxIndividualVariance The most any counter can differ before we give up
// @return ratio of total variance between counters and pattern compared to total pattern size
func PatternMatchVariance(counters, pattern []int, maxIndividualVariance float64) float64 {
	numCounters := len(counters)
	total := 0
	patternLength := 0
	for i := 0; i < numCounters; i++ {
		total += counters[i]
		patternLength += pattern[i]
	}
	if total < patternLength {
		// If we don't even have one pixel per unit of bar width, assume this is too small
		// to reliably match, so fail:
		math.Inf(1)
	}

	unitBarWidth := float64(total) / float64(patternLength)
	maxIndividualVariance *= unitBarWidth

	totalVariance := float64(0)
	for x := 0; x < numCounters; x++ {
		counter := float64(counters[x])
		scaledPattern := float64(pattern[x]) * unitBarWidth
		variance := counter - scaledPattern
		if variance < 0 {
			variance = -variance
		}
		if variance > maxIndividualVariance {
			return math.Inf(1)
		}
		totalVariance += variance
	}
	return totalVariance / float64(total)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
