package qrcode

import (
	"sort"

	"github.com/teldio-operations/gozxing"
)

// noPoints is the result point list of a barcode that has no place in the image: the one that
// processStructuredAppend builds by joining several QR codes together.
var noPoints = []gozxing.ResultPoint{}

// processStructuredAppend joins the QR codes of a structured append series into one result.
//
// The ISO 18004 structured append mode splits one message across several QR codes and numbers
// them. When Decode reads a whole series, the caller wants the message, not the pieces, so the
// pieces are sorted by their sequence number and concatenated. Any QR code in the image that
// carries no sequence number is left alone.
func processStructuredAppend(results []*gozxing.Result) []*gozxing.Result {
	// split the results into the pieces of a series and everything else
	newResults := make([]*gozxing.Result, 0)
	saResults := make([]*gozxing.Result, 0)
	for _, result := range results {
		metadata := result.GetResultMetadata()
		if _, ok := metadata[gozxing.ResultMetadataType_STRUCTURED_APPEND_SEQUENCE]; ok {
			saResults = append(saResults, result)
		} else {
			newResults = append(newResults, result)
		}
	}

	// One piece on its own is left as it is, with its sequence number and its result points. A
	// series exists to spread a message over several images, so the rest of this one is most
	// likely in another image, and only the caller can put them together.
	if len(saResults) < 2 {
		return results
	}
	// sort and concatenate the SA list items
	sort.Slice(saResults, newSAComparator(saResults))
	concatedText := make([]byte, 0)
	rawBytesLen := 0
	byteSegmentLength := 0
	for _, saResult := range saResults {
		concatedText = append(concatedText, []byte(saResult.GetText())...)
		rawBytesLen += len(saResult.GetRawBytes())
		metadata := saResult.GetResultMetadata()
		if byteSegments, ok := metadata[gozxing.ResultMetadataType_BYTE_SEGMENTS].([][]byte); ok {
			for _, segment := range byteSegments {
				byteSegmentLength += len(segment)
			}
		}
	}
	newRawBytes := make([]byte, rawBytesLen)
	newByteSegment := make([]byte, byteSegmentLength)
	newRawBytesIndex := 0
	byteSegmentIndex := 0
	for _, saResult := range saResults {
		copy(newRawBytes[newRawBytesIndex:], saResult.GetRawBytes())
		newRawBytesIndex += len(saResult.GetRawBytes())

		metadata := saResult.GetResultMetadata()
		if byteSegments, ok := metadata[gozxing.ResultMetadataType_BYTE_SEGMENTS].([][]byte); ok {
			for _, segment := range byteSegments {
				copy(newByteSegment[byteSegmentIndex:], segment)
				byteSegmentIndex += len(segment)
			}
		}
	}
	newResult := gozxing.NewResult(string(concatedText), newRawBytes, noPoints, gozxing.BarcodeFormat_QR_CODE)
	if byteSegmentLength > 0 {
		byteSegmentList := [][]byte{newByteSegment}
		newResult.PutMetadata(gozxing.ResultMetadataType_BYTE_SEGMENTS, byteSegmentList)
	}
	newResults = append(newResults, newResult)
	return newResults
}

func newSAComparator(results []*gozxing.Result) func(int, int) bool {
	return func(a, b int) bool {
		aNumber, _ := results[a].GetResultMetadata()[gozxing.ResultMetadataType_STRUCTURED_APPEND_SEQUENCE].(int)
		bNumber, _ := results[b].GetResultMetadata()[gozxing.ResultMetadataType_STRUCTURED_APPEND_SEQUENCE].(int)
		return aNumber < bNumber
	}
}
