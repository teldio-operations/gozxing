package gozxing_test

import (
	"sync"
	"testing"

	"github.com/teldio-operations/gozxing"
	"github.com/teldio-operations/gozxing/oned"
	"github.com/teldio-operations/gozxing/testutil"
)

// TestConcurrentGetBlackRow reads rows of one image from several goroutines at once. Run it under
// -race: the binarizer used to keep the luminance row and the histogram on itself, so two readers
// of the same image wrote over each other's working memory.
func TestConcurrentGetBlackRow(t *testing.T) {
	bmp := testutil.NewBinaryBitmapFromFile("oned/testdata/code128/01.png")
	height := bmp.GetHeight()

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row := gozxing.NewBitArray(bmp.GetWidth())
			for y := 0; y < height; y++ {
				bmp.GetBlackRow(y, row)
			}
		}()
	}
	wg.Wait()
}

// TestConcurrentGetBlackMatrix reads the black matrix of one image from several goroutines at once.
func TestConcurrentGetBlackMatrix(t *testing.T) {
	bmp := testutil.NewBinaryBitmapFromFile("oned/testdata/code128/01.png")

	matrices := make([]*gozxing.BitMatrix, 8)
	var wg sync.WaitGroup
	for worker := range matrices {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			matrices[worker], _ = bmp.GetBlackMatrix()
		}(worker)
	}
	wg.Wait()

	// GetBlackMatrix works the matrix out once and hands the same one to everybody after that.
	for worker, matrix := range matrices {
		if matrix != matrices[0] {
			t.Fatalf("worker %v got a different matrix from worker 0", worker)
		}
	}
}

// TestRotateCounterClockwiseCached checks that turning an image is done once and shared. Several
// readers of one image each turn it when they cannot read it the right way up, and the turned copy
// holds the pixels of the image all over again.
func TestRotateCounterClockwiseCached(t *testing.T) {
	bmp := testutil.NewBinaryBitmapFromFile("oned/testdata/code128/01.png")

	first, e := bmp.RotateCounterClockwise()
	if e != nil {
		t.Fatalf("RotateCounterClockwise: %v", e)
	}
	second, e := bmp.RotateCounterClockwise()
	if e != nil {
		t.Fatalf("RotateCounterClockwise: %v", e)
	}
	if first != second {
		t.Fatal("RotateCounterClockwise turned the image twice, wants the same one back")
	}
	if first.GetWidth() != bmp.GetHeight() || first.GetHeight() != bmp.GetWidth() {
		t.Fatalf("turned image is %vx%v, wants %vx%v",
			first.GetWidth(), first.GetHeight(), bmp.GetHeight(), bmp.GetWidth())
	}

	turned := make([]*gozxing.BinaryBitmap, 8)
	var wg sync.WaitGroup
	for worker := range turned {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			turned[worker], _ = bmp.RotateCounterClockwise()
		}(worker)
	}
	wg.Wait()

	for worker, t2 := range turned {
		if t2 != first {
			t.Fatalf("worker %v got a different turned image", worker)
		}
	}
}

// TestConcurrentReadersOneBitmap runs several readers over one bitmap at the same time, which is
// what a caller does to read an image that may hold barcodes of more than one format. Each reader
// has to report what it reports on its own.
func TestConcurrentReadersOneBitmap(t *testing.T) {
	const file = "oned/testdata/code128/01.png"
	const wants = "005-3379497200006"

	readers := func() []gozxing.Reader {
		return []gozxing.Reader{
			oned.NewCode128Reader(),
			oned.NewCode39Reader(),
			oned.NewCode93Reader(),
			oned.NewCodaBarReader(),
			oned.NewITFReader(),
			oned.NewEAN13Reader(),
		}
	}

	// What each reader reports on a bitmap of its own is the answer to compare against.
	alone := make([]string, len(readers()))
	for i, reader := range readers() {
		results, e := reader.Decode(testutil.NewBinaryBitmapFromFile(file), nil)
		if e == nil && len(results) > 0 {
			alone[i] = results[0].GetText()
		}
	}
	if alone[0] != wants {
		t.Fatalf("the Code 128 reader alone read %q, wants %q", alone[0], wants)
	}

	shared := testutil.NewBinaryBitmapFromFile(file)
	together := make([]string, len(readers()))
	var wg sync.WaitGroup
	for i, reader := range readers() {
		wg.Add(1)
		go func(i int, reader gozxing.Reader) {
			defer wg.Done()
			results, e := reader.Decode(shared, nil)
			if e == nil && len(results) > 0 {
				together[i] = results[0].GetText()
			}
		}(i, reader)
	}
	wg.Wait()

	for i := range alone {
		if together[i] != alone[i] {
			t.Fatalf("reader %v read %q sharing a bitmap, but %q with one of its own",
				i, together[i], alone[i])
		}
	}
}
