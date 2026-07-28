package qrcode

import (
	"strconv"

	"github.com/teldio-operations/gozxing"
	"github.com/teldio-operations/gozxing/common"
	"github.com/teldio-operations/gozxing/common/util"
	"github.com/teldio-operations/gozxing/qrcode/decoder"
	"github.com/teldio-operations/gozxing/qrcode/detector"
)

type QRCodeReader struct {
	decoder *decoder.Decoder
}

func NewQRCodeReader() gozxing.Reader {
	return &QRCodeReader{
		decoder.NewDecoder(),
	}
}

func (this *QRCodeReader) GetDecoder() *decoder.Decoder {
	return this.decoder
}

func (this *QRCodeReader) DecodeWithoutHints(image *gozxing.BinaryBitmap) ([]*gozxing.Result, error) {
	return this.Decode(image, nil)
}

// Decode reads every QR code in the image, in the order the detector located them.
//
// It uses the detector that locates all of the QR codes in an image, so one call reads a page of
// them. A QR code that fails to decode does not stop the ones beside it: Decode reports the codes
// it did read, and only fails when it read none.
//
// The PURE_BARCODE hint is the exception. That hint says the image is one symbol, cropped to its
// edges and aligned to the module grid, so there is nothing to detect and nothing to find beside
// it. Decode then reads that one symbol.
func (this *QRCodeReader) Decode(image *gozxing.BinaryBitmap, hints map[gozxing.DecodeHintType]interface{}) ([]*gozxing.Result, error) {
	blackMatrix, e := image.GetBlackMatrix()
	if e != nil {
		return nil, e
	}

	if _, ok := hints[gozxing.DecodeHintType_PURE_BARCODE]; ok {
		bits, e := this.extractPureBits(blackMatrix)
		if e != nil {
			return nil, e
		}
		decoderResult, e := this.decoder.Decode(bits, hints)
		if e != nil {
			return nil, e
		}
		return []*gozxing.Result{newQRResult(decoderResult, []gozxing.ResultPoint{})}, nil
	}

	detectorResults, e := detector.NewMultiDetector(blackMatrix).DetectMulti(hints)
	if e != nil {
		return nil, e
	}

	results := make([]*gozxing.Result, 0, len(detectorResults))
	var firstReaderError error
	for _, detectorResult := range detectorResults {
		decoderResult, e := this.decoder.Decode(detectorResult.GetBits(), hints)
		if e != nil {
			if _, ok := e.(gozxing.ReaderException); !ok {
				return nil, e
			}
			// One unreadable symbol among several is not the caller's answer. Remember why it
			// failed, in case it turns out to be the only symbol in the image.
			if firstReaderError == nil {
				firstReaderError = e
			}
			continue
		}
		points := detectorResult.GetPoints()
		// If the code was mirrored: swap the bottom-left and the top-right points.
		if metadata, ok := decoderResult.GetOther().(*decoder.QRCodeDecoderMetaData); ok {
			metadata.ApplyMirroredCorrection(points)
		}
		results = append(results, newQRResult(decoderResult, points))
	}

	if len(results) == 0 {
		// The detector that locates several QR codes is not a superset of the one that locates a
		// single code. On a hard image it sometimes settles on finder patterns that decode to
		// nothing while the single-code detector reads the image. Fall back to it, so that
		// reporting several codes never costs the one code the reader found before.
		result, e := this.decodeSingleCode(blackMatrix, hints)
		if e == nil {
			return []*gozxing.Result{result}, nil
		}
		if firstReaderError != nil {
			return nil, firstReaderError
		}
		return nil, e
	}
	return processStructuredAppend(results), nil
}

func (this *QRCodeReader) decodeSingleCode(blackMatrix *gozxing.BitMatrix,
	hints map[gozxing.DecodeHintType]interface{}) (*gozxing.Result, error) {

	detectorResult, e := detector.NewDetector(blackMatrix).Detect(hints)
	if e != nil {
		return nil, e
	}
	decoderResult, e := this.decoder.Decode(detectorResult.GetBits(), hints)
	if e != nil {
		return nil, e
	}
	points := detectorResult.GetPoints()
	// If the code was mirrored: swap the bottom-left and the top-right points.
	if metadata, ok := decoderResult.GetOther().(*decoder.QRCodeDecoderMetaData); ok {
		metadata.ApplyMirroredCorrection(points)
	}
	return newQRResult(decoderResult, points), nil
}

func newQRResult(decoderResult *common.DecoderResult, points []gozxing.ResultPoint) *gozxing.Result {
	result := gozxing.NewResult(decoderResult.GetText(), decoderResult.GetRawBytes(), points, gozxing.BarcodeFormat_QR_CODE)
	byteSegments := decoderResult.GetByteSegments()
	if len(byteSegments) > 0 {
		result.PutMetadata(gozxing.ResultMetadataType_BYTE_SEGMENTS, byteSegments)
	}
	ecLevel := decoderResult.GetECLevel()
	if ecLevel != "" {
		result.PutMetadata(gozxing.ResultMetadataType_ERROR_CORRECTION_LEVEL, ecLevel)
	}
	if decoderResult.HasStructuredAppend() {
		result.PutMetadata(
			gozxing.ResultMetadataType_STRUCTURED_APPEND_SEQUENCE,
			decoderResult.GetStructuredAppendSequenceNumber())
		result.PutMetadata(
			gozxing.ResultMetadataType_STRUCTURED_APPEND_PARITY,
			decoderResult.GetStructuredAppendParity())
	}
	result.PutMetadata(
		gozxing.ResultMetadataType_SYMBOLOGY_IDENTIFIER, "]Q"+strconv.Itoa(decoderResult.GetSymbologyModifier()))
	return result
}

func (this *QRCodeReader) Reset() {
	// do nothing
}

func (this *QRCodeReader) extractPureBits(image *gozxing.BitMatrix) (*gozxing.BitMatrix, error) {

	leftTopBlack := image.GetTopLeftOnBit()
	rightBottomBlack := image.GetBottomRightOnBit()
	if leftTopBlack == nil || rightBottomBlack == nil {
		return nil, gozxing.NewNotFoundException()
	}

	moduleSize, e := this.moduleSize(leftTopBlack, image)
	if e != nil {
		return nil, e
	}

	top := leftTopBlack[1]
	bottom := rightBottomBlack[1]
	left := leftTopBlack[0]
	right := rightBottomBlack[0]

	// Sanity check!
	if left >= right || top >= bottom {
		return nil, gozxing.NewNotFoundException(
			"(left,right)=(%v,%v), (top,bottom)=(%v,%v)", left, right, top, bottom)
	}

	if bottom-top != right-left {
		// Special case, where bottom-right module wasn't black so we found something else in the last row
		// Assume it's a square, so use height as the width
		right = left + (bottom - top)
		if right >= image.GetWidth() {
			// Abort if that would not make sense -- off image
			return nil, gozxing.NewNotFoundException("right = %v, width = %v", right, image.GetWidth())
		}
	}

	matrixWidth := util.MathUtils_Round(float64(right-left+1) / moduleSize)
	matrixHeight := util.MathUtils_Round(float64(bottom-top+1) / moduleSize)
	if matrixWidth <= 0 || matrixHeight <= 0 {
		return nil, gozxing.NewNotFoundException("matrixWidth/Height = %v, %v", matrixWidth, matrixHeight)
	}
	if matrixHeight != matrixWidth {
		// Only possibly decode square regions
		return nil, gozxing.NewNotFoundException("matrixWidth/Height = %v, %v", matrixWidth, matrixHeight)
	}

	// Push in the "border" by half the module width so that we start
	// sampling in the middle of the module. Just in case the image is a
	// little off, this will help recover.
	nudge := int(moduleSize / 2.0)
	top += nudge
	left += nudge

	// But careful that this does not sample off the edge
	// "right" is the farthest-right valid pixel location -- right+1 is not necessarily
	// This is positive by how much the inner x loop below would be too large
	nudgedTooFarRight := left + int(float64(matrixWidth-1)*moduleSize) - right
	if nudgedTooFarRight > 0 {
		if nudgedTooFarRight > nudge {
			// Neither way fits; abort
			return nil, gozxing.NewNotFoundException("Neither way fits")
		}
		left -= nudgedTooFarRight
	}
	// See logic above
	nudgedTooFarDown := top + int(float64(matrixHeight-1)*moduleSize) - bottom
	if nudgedTooFarDown > 0 {
		if nudgedTooFarDown > nudge {
			// Neither way fits; abort
			return nil, gozxing.NewNotFoundException("Neither way fits")
		}
		top -= nudgedTooFarDown
	}

	// Now just read off the bits
	bits, _ := gozxing.NewBitMatrix(matrixWidth, matrixHeight)
	for y := 0; y < matrixHeight; y++ {
		iOffset := top + int(float64(y)*moduleSize)
		for x := 0; x < matrixWidth; x++ {
			if image.Get(left+int(float64(x)*moduleSize), iOffset) {
				bits.Set(x, y)
			}
		}
	}
	return bits, nil
}

func (this *QRCodeReader) moduleSize(leftTopBlack []int, image *gozxing.BitMatrix) (float64, error) {
	height := image.GetHeight()
	width := image.GetWidth()
	x := leftTopBlack[0]
	y := leftTopBlack[1]
	inBlack := true
	transitions := 0
	for x < width && y < height {
		if inBlack != image.Get(x, y) {
			transitions++
			if transitions == 5 {
				break
			}
			inBlack = !inBlack
		}
		x++
		y++
	}
	if x == width || y == height {
		return 0, gozxing.NewNotFoundException()
	}
	return float64(x-leftTopBlack[0]) / 7.0, nil
}
