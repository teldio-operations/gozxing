package gozxing

import "sync"

const (
	LUMINANCE_BITS    = 5
	LUMINANCE_SHIFT   = 8 - LUMINANCE_BITS
	LUMINANCE_BUCKETS = 1 << LUMINANCE_BITS
)

// rowScratch is the working memory of one read. A binarizer lends one out per call rather than
// keeping a single set on itself, so that two goroutines reading rows of the same image do not
// write over each other.
type rowScratch struct {
	luminances []byte
	buckets    []int
}

type GlobalHistogramBinarizer struct {
	source  LuminanceSource
	scratch sync.Pool
}

func NewGlobalHistgramBinarizer(source LuminanceSource) Binarizer {
	binarizer := &GlobalHistogramBinarizer{source: source}
	binarizer.scratch.New = func() interface{} {
		return &rowScratch{buckets: make([]int, LUMINANCE_BUCKETS)}
	}
	return binarizer
}

// takeScratch borrows working memory that holds a row of the given width, with a zeroed histogram.
func (this *GlobalHistogramBinarizer) takeScratch(width int) *rowScratch {
	scratch := this.scratch.Get().(*rowScratch)
	if len(scratch.luminances) < width {
		scratch.luminances = make([]byte, width)
	}
	for i := range scratch.buckets {
		scratch.buckets[i] = 0
	}
	return scratch
}

func (this *GlobalHistogramBinarizer) GetLuminanceSource() LuminanceSource {
	return this.source
}

func (this *GlobalHistogramBinarizer) GetWidth() int {
	return this.source.GetWidth()
}

func (this *GlobalHistogramBinarizer) GetHeight() int {
	return this.source.GetHeight()
}

func (this *GlobalHistogramBinarizer) GetBlackRow(y int, row *BitArray) (*BitArray, error) {
	source := this.GetLuminanceSource()
	width := source.GetWidth()
	if row == nil || row.GetSize() < width {
		row = NewBitArray(width)
	} else {
		row.Clear()
	}

	scratch := this.takeScratch(width)
	defer this.scratch.Put(scratch)

	localLuminances, e := source.GetRow(y, scratch.luminances)
	if e != nil {
		return nil, e
	}
	localBuckets := scratch.buckets
	for x := 0; x < width; x++ {
		localBuckets[(localLuminances[x]&0xff)>>LUMINANCE_SHIFT]++
	}
	blackPoint, e := this.estimateBlackPoint(localBuckets)
	if e != nil {
		return nil, e
	}

	if width < 3 {
		// Special case for very small images
		for x := 0; x < width; x++ {
			if int(localLuminances[x]&0xff) < blackPoint {
				row.Set(x)
			}
		}
	} else {
		left := int(localLuminances[0] & 0xff)
		center := int(localLuminances[1] & 0xff)
		for x := 1; x < width-1; x++ {
			right := int(localLuminances[x+1] & 0xff)
			// A simple -1 4 -1 box filter with a weight of 2.
			if ((center*4)-left-right)/2 < blackPoint {
				row.Set(x)
			}
			left = center
			center = right
		}
	}
	return row, nil
}

func (this *GlobalHistogramBinarizer) GetBlackMatrix() (*BitMatrix, error) {
	source := this.GetLuminanceSource()
	width := source.GetWidth()
	height := source.GetHeight()
	matrix, e := NewBitMatrix(width, height)
	if e != nil {
		return nil, e
	}

	// Quickly calculates the histogram by sampling four rows from the image. This proved to be
	// more robust on the blackbox tests than sampling a diagonal as we used to do.
	scratch := this.takeScratch(width)
	defer this.scratch.Put(scratch)

	localBuckets := scratch.buckets
	for y := 1; y < 5; y++ {
		row := height * y / 5
		localLuminances, _ := source.GetRow(row, scratch.luminances)
		right := (width * 4) / 5
		for x := width / 5; x < right; x++ {
			pixel := localLuminances[x] & 0xff
			localBuckets[pixel>>LUMINANCE_SHIFT]++
		}
	}
	blackPoint, e := this.estimateBlackPoint(localBuckets)
	if e != nil {
		return nil, e
	}

	// We delay reading the entire image luminance until the black point estimation succeeds.
	// Although we end up reading four rows twice, it is consistent with our motto of
	// "fail quickly" which is necessary for continuous scanning.
	localLuminances := source.GetMatrix()
	for y := 0; y < height; y++ {
		offset := y * width
		for x := 0; x < width; x++ {
			pixel := int(localLuminances[offset+x] & 0xff)
			if pixel < blackPoint {
				matrix.Set(x, y)
			}
		}
	}

	return matrix, nil
}

func (this *GlobalHistogramBinarizer) CreateBinarizer(source LuminanceSource) Binarizer {
	return NewGlobalHistgramBinarizer(source)
}

func (this *GlobalHistogramBinarizer) estimateBlackPoint(buckets []int) (int, error) {
	// Find the tallest peak in the histogram.
	numBuckets := len(buckets)
	maxBucketCount := 0
	firstPeak := 0
	firstPeakSize := 0
	for x := 0; x < numBuckets; x++ {
		if buckets[x] > firstPeakSize {
			firstPeak = x
			firstPeakSize = buckets[x]
		}
		if buckets[x] > maxBucketCount {
			maxBucketCount = buckets[x]
		}
	}

	// Find the second-tallest peak which is somewhat far from the tallest peak.
	secondPeak := 0
	secondPeakScore := 0
	for x := 0; x < numBuckets; x++ {
		distanceToBiggest := x - firstPeak
		// Encourage more distant second peaks by multiplying by square of distance.
		score := buckets[x] * distanceToBiggest * distanceToBiggest
		if score > secondPeakScore {
			secondPeak = x
			secondPeakScore = score
		}
	}

	// Make sure firstPeak corresponds to the black peak.
	if firstPeak > secondPeak {
		firstPeak, secondPeak = secondPeak, firstPeak
	}

	// If there is too little contrast in the image to pick a meaningful black point, throw rather
	// than waste time trying to decode the image, and risk false positives.
	if secondPeak-firstPeak <= numBuckets/16 {
		return 0, NewNotFoundException()
	}

	// Find a valley between them that is low and closer to the white peak.
	bestValley := secondPeak - 1
	bestValleyScore := -1
	for x := secondPeak - 1; x > firstPeak; x-- {
		fromFirst := x - firstPeak
		score := fromFirst * fromFirst * (secondPeak - x) * (maxBucketCount - buckets[x])
		if score > bestValleyScore {
			bestValley = x
			bestValleyScore = score
		}
	}

	return bestValley << LUMINANCE_SHIFT, nil
}
