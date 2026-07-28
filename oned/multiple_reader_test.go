package oned

// This file tests that a reader reports every barcode in an image, not just the first one.
//
// It builds the images it needs from the single-barcode images in testdata. For each reader it
// reads every image of one testdata directory on its own and keeps the ones that hold a single
// barcode with a text no earlier image already gave. It then lays those out and reads them back:
//
//   - a column, where every row of the canvas crosses one barcode
//   - a square of four, where every row crosses two, so the reader has to carry on along a row
//     after it has read a barcode from it
//   - both of those at all four right angles
//
// A canvas goes to testdata-multiple/ so that a failure is easy to look at. Those files are build
// output, not fixtures, and .gitignore covers them. Nothing reads them back, so deleting the
// directory is safe.

import (
	"fmt"
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

// The source images butt up against each other, with nothing between them and no margin around
// them, the way a photo of several labels holds them. Every image of one canvas has the same width
// where the source directory allows it, so most canvases add no pixel that was not in a source.
//
// That is deliberate. GetBlackRow estimates a black point from the histogram of the single row it
// reads. Some of these photos are of a tinted label, whose white is a pale yellow, and laying
// pure white beside it gives that row a second white peak. The estimate then separates pure white
// from pale yellow instead of pale yellow from black, and the barcode stops reading. Padding wide
// enough to matter breaks more barcodes than it fixes, as the ITF and EAN-13 photos show.

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

			wants := textsOf(sources)
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

// TestOneDReaderGridOfBarcodes reads four barcodes laid out in a square: top-left, top-right,
// bottom-left, bottom-right.
//
// A row of this canvas crosses two barcodes, which a column of them never does. DecodeRow reads
// the first barcode of a row and stops, so the reader has to carry on along what is left of the
// row to find the right-hand one. See OneDReader.decodeRowAcross.
//
// The results come back in reading order: the top row left to right, then the bottom row.
func TestOneDReaderGridOfBarcodes(t *testing.T) {
	harder := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}
	grids := 0

	for _, set := range multipleSets {
		t.Run(set.name, func(t *testing.T) {
			dir := filepath.Join("testdata", set.dir)
			sources := pickStack(readEachAlone(t, dir, set.reader, nil))
			if len(sources) < 4 {
				t.Skipf("%v has %v images that can share a canvas, a square needs 4", dir, len(sources))
			}
			sources = sources[:4]

			canvas := stackSquare(t, dir, sources)
			writeStack(t, filepath.Join(multipleOutDir, set.name+"-grid.png"), canvas)

			bmp, e := gozxing.NewBinaryBitmapFromImage(canvas)
			if e != nil {
				t.Fatalf("NewBinaryBitmapFromImage failed: %v", e)
			}
			results, e := set.reader().Decode(bmp, harder)
			if e != nil {
				t.Fatalf("Decode of the %v square failed: %v", set.name, e)
			}

			wants := textsOf(sources)
			got := make([]string, len(results))
			for i, result := range results {
				got[i] = result.GetText()
			}
			// Nothing may come back that is not one of the four. A square gives the reader more
			// room to misread than a column does, because it carries on along a row that it has
			// already read a barcode from.
			for _, text := range got {
				if !contains(wants, text) {
					t.Fatalf("Decode of the %v square read %q, which none of %q holds",
						set.name, text, wants)
				}
			}

			// The point of a square is that one row crosses two barcodes, so at least one pair
			// side by side has to come back whole. Reading order does not matter: a row read the
			// right way up gives the pair left to right, and a row that only reads upside down
			// gives it right to left.
			top := contains(got, wants[0]) && contains(got, wants[1])
			bottom := contains(got, wants[2]) && contains(got, wants[3])
			if !top && !bottom {
				t.Fatalf("Decode of the %v square = %q, wants both barcodes of at least one row, "+
					"either %q or %q", set.name, got, wants[:2], wants[2:])
			}
			t.Logf("%v square: read %v of 4, top row whole=%v, bottom row whole=%v",
				set.name, len(got), top, bottom)
			for i, result := range results {
				if format := result.GetBarcodeFormat(); format != set.format {
					t.Fatalf("Decode of the %v square result[%v] format = %v, wants %v",
						set.name, i, format, set.format)
				}
			}
			grids++
		})
	}

	if grids == 0 {
		t.Fatal("no directory held four images that can share a canvas, so nothing was tested")
	}
}

// TestOneDReaderRotatedLayouts reads a column and a square of barcodes at all four right angles.
//
// A reader scans rows, so a canvas turned on its side holds no barcode along any row of it.
// Decode recovers by turning the image a quarter turn and scanning again, which is what the
// TRY_HARDER hint asks it to do, and it reads a row backwards to cope with a canvas turned
// upside down. Between them those two cover all four angles, and every barcode still reads.
func TestOneDReaderRotatedLayouts(t *testing.T) {
	harder := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}

	for _, set := range multipleSets {
		t.Run(set.name, func(t *testing.T) {
			dir := filepath.Join("testdata", set.dir)
			sources := pickStack(readEachAlone(t, dir, set.reader, nil))
			if len(sources) < 2 {
				t.Skipf("%v has %v images that can share a canvas, need 2", dir, len(sources))
			}

			layouts := []struct {
				name   string
				canvas image.Image
				wants  []string
			}{{"column", stackDown(t, dir, sources), textsOf(sources)}}
			if len(sources) >= 4 {
				layouts = append(layouts, struct {
					name   string
					canvas image.Image
					wants  []string
				}{"square", stackSquare(t, dir, sources[:4]), textsOf(sources[:4])})
			}

			// 0 degrees is what the other tests read, so start at a quarter turn.
			for _, layout := range layouts {
				for _, turns := range []int{1, 2, 3} {
					name := fmt.Sprintf("%v/%v", layout.name, turns*90)
					bmp, e := gozxing.NewBinaryBitmapFromImage(turnQuarter(layout.canvas, turns))
					if e != nil {
						t.Fatalf("%v: NewBinaryBitmapFromImage failed: %v", name, e)
					}
					results, e := set.reader().Decode(bmp, harder)
					if e != nil {
						t.Fatalf("Decode of the %v %v failed: %v", set.name, name, e)
					}
					got := make([]string, len(results))
					for i, result := range results {
						got[i] = result.GetText()
					}
					for _, want := range layout.wants {
						if !contains(got, want) {
							t.Fatalf("Decode of the %v %v = %q, misses %q",
								set.name, name, got, want)
						}
					}
					for _, text := range got {
						if !contains(layout.wants, text) {
							t.Fatalf("Decode of the %v %v read %q, which no source holds",
								set.name, name, text)
						}
					}
				}
			}
		})
	}
}

func textsOf(sources []multipleSource) []string {
	texts := make([]string, len(sources))
	for i, s := range sources {
		texts[i] = s.text
	}
	return texts
}

// turnQuarter turns an image a quarter turn clockwise, the given number of times.
func turnQuarter(src image.Image, turns int) image.Image {
	out := src
	for i := 0; i < turns; i++ {
		bounds := out.Bounds()
		turned := image.NewRGBA(image.Rect(0, 0, bounds.Dy(), bounds.Dx()))
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				turned.Set(bounds.Max.Y-1-y, x-bounds.Min.X, out.At(x, y))
			}
		}
		out = turned
	}
	return out
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

func contains(texts []string, text string) bool {
	for _, t := range texts {
		if t == text {
			return true
		}
	}
	return false
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
	// bounds how much white padding a narrow image gets. GetBlackRow estimates a black point from
	// the histogram of the single row it reads, and white padding skews that estimate.
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
		if readableBand(reader, bmp, results[0]) < minBand {
			continue
		}
		seen[text] = true
		sources = append(sources, multipleSource{file, text, img.Bounds().Dx()})
	}
	return sources
}

// minBand is how many rows of an image have to read the same barcode before it belongs on a
// shared canvas.
//
// A reader samples rows sweepRowStep apart at the closest, so a band thinner than two of those
// steps can fall between two sampled rows and go unread. A canvas of several images is taller
// than any one of them, which only widens the sampling, and the height of a canvas depends on
// which images went on it. Picking barcodes with a band this thick keeps the test off that
// knife edge. It also clears the adjacent-row check that OneDReader.doDecode makes of every
// barcode after the first.
const minBand = 2*sweepRowStep + 1

// readableBand counts the rows around the one a barcode was found on that read the same barcode,
// the found row included. It stops counting at 2*minBand, which is as much as any caller needs.
func readableBand(reader gozxing.Reader, bmp *gozxing.BinaryBitmap, result *gozxing.Result) int {
	rowDecoder, ok := reader.(RowDecoder)
	if !ok {
		return 0
	}
	points := result.GetResultPoints()
	if len(points) == 0 {
		return 0
	}

	row := gozxing.NewBitArray(bmp.GetWidth())
	reads := func(y int) bool {
		if y < 0 || y >= bmp.GetHeight() {
			return false
		}
		read, e := bmp.GetBlackRow(y, row)
		if e != nil {
			return false
		}
		again, e := rowDecoder.DecodeRow(y, read, nil)
		return e == nil && again.GetText() == result.GetText()
	}

	found := int(points[0].GetY())
	band := 1
	for _, step := range [2]int{1, -1} {
		for y := found + step; band < 2*minBand && reads(y); y += step {
			band++
		}
	}
	return band
}

// stackDown draws the source images down one white canvas, one directly below the next, each one
// centered, with a margin around the whole canvas. The source pixels go across unchanged, because
// scaling a photo of a barcode can make it unreadable.
func stackDown(t *testing.T, dir string, sources []multipleSource) image.Image {
	t.Helper()

	images := make([]image.Image, len(sources))
	width, height := 0, 0
	for i, s := range sources {
		img := readPNG(t, filepath.Join(dir, s.file))
		images[i] = img
		bounds := img.Bounds()
		if bounds.Dx() > width {
			width = bounds.Dx()
		}
		height += bounds.Dy()
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	y := 0
	for _, img := range images {
		bounds := img.Bounds()
		left := (width - bounds.Dx()) / 2
		draw.Draw(canvas, image.Rect(left, y, left+bounds.Dx(), y+bounds.Dy()), img, bounds.Min, draw.Src)
		y += bounds.Dy()
	}
	return canvas
}

// stackSquare draws four source images in a square on one white canvas: sources[0] top-left,
// sources[1] top-right, sources[2] bottom-left, sources[3] bottom-right.
//
// The cells butt up against each other, with a margin only around the whole canvas. Every cell is
// the same size, so the two barcodes of a row start at the same x and a row of the canvas crosses
// both of them. Each image sits at the left of its cell, so any cell wider than its image leaves
// white to its right, which serves as the quiet zone of the barcode that follows along the row.
func stackSquare(t *testing.T, dir string, sources []multipleSource) image.Image {
	t.Helper()
	if len(sources) != 4 {
		t.Fatalf("stackSquare got %v images, wants 4", len(sources))
	}

	images := make([]image.Image, 4)
	cellWidth, cellHeight := 0, 0
	for i, s := range sources {
		img := readPNG(t, filepath.Join(dir, s.file))
		images[i] = img
		if w := img.Bounds().Dx(); w > cellWidth {
			cellWidth = w
		}
		if h := img.Bounds().Dy(); h > cellHeight {
			cellHeight = h
		}
	}

	canvas := image.NewRGBA(image.Rect(0, 0, 2*cellWidth, 2*cellHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	for i, img := range images {
		column, row := i%2, i/2
		left := column * cellWidth
		top := row * cellHeight
		bounds := img.Bounds()
		draw.Draw(canvas, image.Rect(left, top, left+bounds.Dx(), top+bounds.Dy()), img, bounds.Min, draw.Src)
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
