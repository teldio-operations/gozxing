package oned

// This file tests that a reader reports every barcode in an image, not just the first one.
//
// It builds the images it needs from the single-barcode images in testdata. For each reader it
// reads every image of one testdata directory on its own, keeps the ones that hold a single
// barcode with a text no earlier image already gave, and draws them down one tall canvas. It
// then reads that canvas back and expects one result per barcode, from the top down.
//
// The canvas goes to testdata-multiple/ so that a failure is easy to look at. Those files are
// build output, not fixtures, and .gitignore covers them. Nothing reads them back, so deleting
// the directory is safe.

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/teldio-operations/gozxing"
)

// multipleOutDir holds the stacked images. See the note at the top of this file.
const multipleOutDir = "testdata-multiple"

// multipleGap is the white band between two source images on the canvas, in pixels. It keeps the
// 8x8 blocks that HybridBinarizer works on from covering two source images at once.
//
// There is no band to the left or right of a source image. GetBlackRow estimates a black point
// from the histogram of the single row it reads, and padding a narrow image out to a wider canvas
// fills that row with white and skews the estimate. Every image of one stack has the same width
// for the same reason, so none of them needs padding.
const multipleGap = 16

var multipleSets = []struct {
	name   string
	dir    string
	reader func() gozxing.Reader
	format gozxing.BarcodeFormat
}{
	{"codabar", "codabar", NewCodaBarReader, gozxing.BarcodeFormat_CODABAR},
	{"code39", "code39", NewCode39Reader, gozxing.BarcodeFormat_CODE_39},
	{"code93", "code93", NewCode93Reader, gozxing.BarcodeFormat_CODE_93},
	{"code128", "code128", NewCode128Reader, gozxing.BarcodeFormat_CODE_128},
	{"ean8", "ean8", NewEAN8Reader, gozxing.BarcodeFormat_EAN_8},
	{"ean13", "ean13", NewEAN13Reader, gozxing.BarcodeFormat_EAN_13},
	{"itf", "itf", NewITFReader, gozxing.BarcodeFormat_ITF},
	{"upca", "upca", NewUPCAReader, gozxing.BarcodeFormat_UPC_A},
	{"upce", "upce", NewUPCEReader, gozxing.BarcodeFormat_UPC_E},
}

func TestOneDReaderMultipleBarcodes(t *testing.T) {
	harder := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}

	for _, set := range multipleSets {
		t.Run(set.name, func(t *testing.T) {
			dir := filepath.Join("testdata", set.dir)
			sources := pickStack(readEachAlone(t, dir, set.reader, nil))
			if len(sources) < 2 {
				t.Fatalf("only %v image of %v can go on one canvas, need 2", len(sources), dir)
			}

			t.Logf("stacked %v barcodes from %v", len(sources), dir)
			canvas := stackDown(t, dir, sources)
			writeStack(t, filepath.Join(multipleOutDir, set.name+".png"), canvas)

			bmp, e := gozxing.NewBinaryBitmapFromImage(canvas)
			if e != nil {
				t.Fatalf("NewBinaryBitmapFromImage failed: %v", e)
			}
			results, e := set.reader().Decode(bmp, harder)
			if e != nil {
				t.Fatalf("Decode of the %v stack failed: %v", set.name, e)
			}

			wants := make([]string, len(sources))
			for i, s := range sources {
				wants[i] = s.text
			}
			got := make([]string, len(results))
			for i, result := range results {
				got[i] = result.GetText()
			}

			if len(got) < 2 {
				t.Fatalf("Decode of the %v stack of %v barcodes = %q, wants more than one result",
					set.name, len(sources), got)
			}
			if !reflect.DeepEqual(got, wants) {
				t.Fatalf("Decode of the %v stack = %q, wants %q", set.name, got, wants)
			}
			for i, result := range results {
				if format := result.GetBarcodeFormat(); format != set.format {
					t.Fatalf("Decode of the %v stack result[%v] format = %v, wants %v",
						set.name, i, format, set.format)
				}
			}
		})
	}
}

// TestOneDReaderSingleBarcodePerImage reads every image of testdata and expects at most one
// barcode. Each of those images is a photo or a drawing of one barcode, so a second result means
// the reader misread a row.
//
// The per-format tests already assert the text of 154 of these images. This one also covers the
// six that upstream zxing cannot read, where the reader has to find nothing rather than find
// something wrong, and it runs with and without TRY_HARDER.
func TestOneDReaderSingleBarcodePerImage(t *testing.T) {
	harder := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}

	// These images do hold, or do read as, more than one barcode.
	knownMultiple := map[string]string{
		// Two photos of the same kind of label, each of which caught the label below it as
		// well. The lower barcode of both reads what the label prints above it.
		// TestCode39Reader asserts the two texts of 08.png.
		"testdata/code39/02.png": "a second label at the bottom edge, 001EC947D49B",
		"testdata/code39/08.png": "a second label at the bottom edge, 001EC9476B0A",

		// Glare washes out the right half of this barcode, and upstream zxing cannot read it.
		// The reader reports two candidates that both pass the check digit: 070097026788, which
		// is what it read before it could return more than one and is wrong, and 070097025088,
		// which is what the label prints.
		"testdata/upca/4.png": "one misread and one correct read of a damaged barcode",
	}

	for _, set := range multipleSets {
		t.Run(set.name, func(t *testing.T) {
			dir := filepath.Join("testdata", set.dir)
			for _, file := range listPNGs(t, dir) {
				path := filepath.Join(dir, file)
				if reason, ok := knownMultiple[filepath.ToSlash(path)]; ok {
					t.Logf("skipping %v: %v", path, reason)
					continue
				}
				img := readPNG(t, path)
				for _, hints := range []map[gozxing.DecodeHintType]interface{}{nil, harder} {
					bmp, e := gozxing.NewBinaryBitmapFromImage(img)
					if e != nil {
						t.Fatalf("NewBinaryBitmapFromImage(%v) failed: %v", path, e)
					}
					results, e := set.reader().Decode(bmp, hints)
					if e != nil {
						continue // nothing found, which the per-format tests cover
					}
					if len(results) > 1 {
						texts := make([]string, len(results))
						for i, result := range results {
							texts[i] = result.GetText()
						}
						t.Errorf("Decode(%v, tryHarder=%v) = %q, wants one barcode",
							path, hints != nil, texts)
					}
				}
			}
		})
	}
}

type multipleSource struct {
	file  string
	text  string
	width int
}

const (
	// maxStack is how many barcodes go on one canvas. A handful proves that the reader reports
	// every one of them, and a stack of every image of a directory only makes the test slow.
	maxStack = 8

	// minWidthRatio is the narrowest source, relative to the canvas, that a stack accepts. It
	// bounds how much white padding the narrow images get. See multipleGap.
	minWidthRatio = 0.75
)

// pickStack chooses the sources to stack, in file order.
//
// It prefers the largest set of sources that already share a width, because those need no
// padding at all. Some directories hold no two images of the same width, and for those it falls
// back to the canvas width that fits the most sources without padding any of them past
// minWidthRatio.
func pickStack(sources []multipleSource) []multipleSource {
	best := widestFit(sources, func(s multipleSource, width int) bool {
		return s.width == width
	})
	if len(best) < 2 {
		best = widestFit(sources, func(s multipleSource, width int) bool {
			return s.width <= width && float64(s.width) >= minWidthRatio*float64(width)
		})
	}
	if len(best) > maxStack {
		best = best[:maxStack]
	}
	return best
}

// widestFit returns the largest set of sources that one canvas width holds, by the caller's rule.
// A tie goes to the wider canvas, whose images carry more pixels per bar and so read better.
func widestFit(sources []multipleSource, fits func(multipleSource, int) bool) []multipleSource {
	best := []multipleSource(nil)
	bestWidth := 0
	for _, candidate := range sources {
		group := make([]multipleSource, 0, len(sources))
		for _, s := range sources {
			if fits(s, candidate.width) {
				group = append(group, s)
			}
		}
		if len(group) > len(best) || (len(group) == len(best) && candidate.width > bestWidth) {
			best = group
			bestWidth = candidate.width
		}
	}
	return best
}

// readEachAlone reads every image of dir on its own and returns those that hold exactly one
// barcode whose text no earlier image of the directory already gave. Two barcodes with the same
// text are one result, so a stack has to hold texts that differ.
//
// The caller passes no hints, which keeps the blurred photos that only read with TRY_HARDER out
// of the stacks. This test is about how many barcodes a reader reports, not about how well it
// reads a hard image.
func readEachAlone(t *testing.T, dir string, newReader func() gozxing.Reader,
	hints map[gozxing.DecodeHintType]interface{}) []multipleSource {
	t.Helper()

	seen := make(map[string]bool)
	sources := make([]multipleSource, 0)
	for _, file := range listPNGs(t, dir) {
		img := readPNG(t, filepath.Join(dir, file))
		bmp, e := gozxing.NewBinaryBitmapFromImage(img)
		if e != nil {
			continue
		}
		reader := newReader()
		results, e := reader.Decode(bmp, hints)
		if e != nil || len(results) != 1 {
			continue
		}
		text := results[0].GetText()
		if text == "" || seen[text] {
			continue
		}
		if !readsOnAdjacentRow(reader, bmp, results[0]) {
			continue
		}
		seen[text] = true
		sources = append(sources, multipleSource{file, text, img.Bounds().Dx()})
	}
	return sources
}

// readsOnAdjacentRow reports whether the barcode reads the same on a row next to the one it was
// found on.
//
// A reader takes the first barcode of an image as it is, but a barcode found after that has to
// repeat on an adjacent row, so that a misread of one noisy row stays out of the results. Only a
// source that clears that bar belongs in a stack, where it is one of several barcodes. See
// OneDReader.doDecode.
func readsOnAdjacentRow(reader gozxing.Reader, bmp *gozxing.BinaryBitmap, result *gozxing.Result) bool {
	rowDecoder, ok := reader.(RowDecoder)
	if !ok {
		return false
	}
	points := result.GetResultPoints()
	if len(points) == 0 {
		return false
	}

	found := int(points[0].GetY())
	row := gozxing.NewBitArray(bmp.GetWidth())
	for _, adjacent := range [2]int{found + 1, found - 1} {
		if adjacent < 0 || adjacent >= bmp.GetHeight() {
			continue
		}
		read, e := bmp.GetBlackRow(adjacent, row)
		if e != nil {
			continue
		}
		again, e := rowDecoder.DecodeRow(adjacent, read, nil)
		if e == nil && again.GetText() == result.GetText() {
			return true
		}
	}
	return false
}

// stackDown draws the source images down one white canvas, each one centered, with a white
// margin around it. The source pixels go across unchanged, because scaling a photo of a barcode
// can make it unreadable.
func stackDown(t *testing.T, dir string, sources []multipleSource) image.Image {
	t.Helper()

	images := make([]image.Image, len(sources))
	width, height := 0, multipleGap
	for i, s := range sources {
		img := readPNG(t, filepath.Join(dir, s.file))
		images[i] = img
		bounds := img.Bounds()
		if bounds.Dx() > width {
			width = bounds.Dx()
		}
		height += bounds.Dy() + multipleGap
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	y := multipleGap
	for _, img := range images {
		bounds := img.Bounds()
		left := (width - bounds.Dx()) / 2
		draw.Draw(canvas, image.Rect(left, y, left+bounds.Dx(), y+bounds.Dy()), img, bounds.Min, draw.Src)
		y += bounds.Dy() + multipleGap
	}
	return canvas
}

// writeStack saves the canvas for a person to look at. A checkout that cannot be written to is
// not a test failure, so it only logs what went wrong.
func writeStack(t *testing.T, path string, img image.Image) {
	t.Helper()

	e := os.MkdirAll(filepath.Dir(path), 0o755)
	if e != nil {
		t.Logf("could not create %v: %v", filepath.Dir(path), e)
		return
	}
	file, e := os.Create(path)
	if e != nil {
		t.Logf("could not create %v: %v", path, e)
		return
	}
	defer file.Close()
	e = png.Encode(file, img)
	if e != nil {
		t.Logf("could not write %v: %v", path, e)
	}
}

var leadingDigits = regexp.MustCompile(`^[0-9]+`)

// listPNGs returns the PNG files of a directory, ordered so that "2.png" comes before "10.png".
func listPNGs(t *testing.T, dir string) []string {
	t.Helper()

	entries, e := os.ReadDir(dir)
	if e != nil {
		t.Fatalf("could not read %v: %v", dir, e)
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".png" {
			files = append(files, entry.Name())
		}
	}
	sort.SliceStable(files, func(i, j int) bool {
		ni, oki := numericPrefix(files[i])
		nj, okj := numericPrefix(files[j])
		if oki && okj && ni != nj {
			return ni < nj
		}
		if oki != okj {
			return oki
		}
		return files[i] < files[j]
	})
	return files
}

func numericPrefix(name string) (int, bool) {
	digits := leadingDigits.FindString(name)
	if digits == "" {
		return 0, false
	}
	n, e := strconv.Atoi(digits)
	if e != nil {
		return 0, false
	}
	return n, true
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()

	file, e := os.Open(path)
	if e != nil {
		t.Fatalf("could not open %v: %v", path, e)
	}
	defer file.Close()
	img, e := png.Decode(file)
	if e != nil {
		t.Fatalf("could not decode %v: %v", path, e)
	}
	return img
}
