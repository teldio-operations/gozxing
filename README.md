# gozxing A Barcode Scanning/Encoding Library for Go

[![Build Status](https://github.com/makiuchi-d/gozxing/actions/workflows/main.yml/badge.svg)](https://github.com/makiuchi-d/gozxing/actions/workflows/main.yml)
[![codecov](https://codecov.io/gh/makiuchi-d/gozxing/branch/master/graph/badge.svg)](https://codecov.io/gh/makiuchi-d/gozxing)

[ZXing](https://github.com/zxing/zxing) is an open-source, multi-format 1D/2D barcode image processing library for Java.
This project is a port of ZXing core library to pure Go.

## About this fork

This is Teldio's fork of [makiuchi-d/gozxing](https://github.com/makiuchi-d/gozxing). Import it as:

```Go
import "github.com/teldio-operations/gozxing"
```

The two badges above report the state of the upstream project, not of this fork.

### What this fork changes

**`Reader.Decode` returns every barcode in the image.** It returns a `[]*Result` rather than a
single `*Result`, and it is a breaking change: a caller that wants one barcode reads element 0.
Upstream stopped at the first barcode it found, so a photo of a shelf, a box of labels, or a page
of QR codes gave one result no matter how many it held.

- The readers in `oned` scan the whole image and read barcodes stacked down it or laid out side by
  side, and report them from the top of the image down. See
  [Scanning several barcodes in one image](#scanning-several-barcodes-in-one-image).
- `qrcode.QRCodeReader` reads every QR code in the image. Upstream needed a second reader type from
  a separate `multi` package for that, and this fork has no `multi` package: `Reader.Decode`
  returns a list, so a second interface earned nothing. `multi.MultipleBarcodeReader` and
  `multi/qrcode.QRCodeMultiReader` are gone, and `multi/qrcode/detector` moved to
  `qrcode/detector`.
- The Aztec and Data Matrix readers return at most one result. Their detectors find one symbol per
  image, so one is all they can find.

**Reading a barcode that is not the first one found is stricter.** The first barcode of an image is
reported as upstream reported it. Every barcode after that has to read the same on an adjacent row
before it counts, which keeps a misread of one blurred or noisy row out of the results. Scanning
more of an image finds more real barcodes, and it also finds more ways to be wrong.

**A `BinaryBitmap` is safe to share between goroutines.** See [Thread Safety](#thread-safety).

## Porting Status (supported formats)

### 2D barcodes

| Format      | Scanning           | Encoding           |
|-------------|--------------------|--------------------|
| QR Code     | :heavy_check_mark: | :heavy_check_mark: |
| Data Matrix | :heavy_check_mark: | :heavy_check_mark: |
| Aztec       | :heavy_check_mark: |                    |
| PDF 417     |                    |                    |
| MaxiCode    |                    |                    |


### 1D product barcodes

| Format      | Scanning           | Encoding           |
|-------------|--------------------|--------------------|
| UPC-A       | :heavy_check_mark: | :heavy_check_mark: |
| UPC-E       | :heavy_check_mark: | :heavy_check_mark: |
| EAN-8       | :heavy_check_mark: | :heavy_check_mark: |
| EAN-13      | :heavy_check_mark: | :heavy_check_mark: |

### 1D industrial barcode

| Format       | Scanning           | Encoding           |
|--------------|--------------------|--------------------|
| Code 39      | :heavy_check_mark: | :heavy_check_mark: |
| Code 93      | :heavy_check_mark: | :heavy_check_mark: |
| Code 128     | :heavy_check_mark: | :heavy_check_mark: |
| Codabar      | :heavy_check_mark: | :heavy_check_mark: |
| ITF          | :heavy_check_mark: | :heavy_check_mark: |
| RSS-14       | :heavy_check_mark: | -                  |
| RSS-Expanded |                    |                    |

### Special reader/writer

| Reader/Writer                | Porting status     |
|------------------------------|--------------------|
| MultiFormatReader            |                    |
| MultiFormatWriter            |                    |
| ByQuadrantReader             |                    |
| GenericMultipleBarcodeReader |                    |
| QRCodeMultiReader            | :heavy_check_mark: |
| MultiFormatUPCEANReader      | :heavy_check_mark: |
| MultiFormatOneDReader        |                    |

## Usage Examples

### Scanning QR code

```Go
package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	"os"

	"github.com/teldio-operations/gozxing"
	"github.com/teldio-operations/gozxing/qrcode"
)

func main() {
	// open and decode image file
	file, _ := os.Open("qrcode.jpg")
	img, _, _ := image.Decode(file)

	// prepare BinaryBitmap
	bmp, _ := gozxing.NewBinaryBitmapFromImage(img)

	// decode image
	qrReader := qrcode.NewQRCodeReader()
	results, _ := qrReader.Decode(bmp, nil)

	// Decode returns one result per barcode it finds in the image
	for _, result := range results {
		fmt.Println(result)
	}
}
```

### Scanning several barcodes in one image

`Reader.Decode` returns a `[]*gozxing.Result`, one entry per barcode. The 1D readers in
`oned` scan the whole image and report every barcode they find, from the top of the image
down. The 2D readers report at most one, because their detectors locate one symbol. Use
`multi/qrcode.QRCodeMultiReader` for an image of several QR codes.

```Go
	reader := oned.NewCode128Reader()
	results, err := reader.Decode(bmp, nil)
	if err != nil {
		// gozxing.NotFoundException when the image holds no barcode at all
		log.Fatal(err)
	}
	for _, result := range results {
		fmt.Println(result.GetText())
	}
```

### Generating CODE128 barcode

```Go
package main

import (
	"image/png"
	"os"

	"github.com/teldio-operations/gozxing"
	"github.com/teldio-operations/gozxing/oned"
)

func main() {
	// Generate a barcode image (*BitMatrix)
	enc := oned.NewCode128Writer()
	img, _ := enc.Encode("Hello, Gophers!", gozxing.BarcodeFormat_CODE_128, 250, 50, nil)

	file, _ := os.Create("barcode.png")
	defer file.Close()

	// *BitMatrix implements the image.Image interface,
	// so it is able to be passed to png.Encode directly.
	_ = png.Encode(file, img)
}
```

## Thread Safety

A `BinaryBitmap` is safe to share between goroutines. Several goroutines may call `GetBlackMatrix`
and `GetBlackRow` on one instance with no locking of their own. `GetBlackMatrix` works the matrix
out once and hands the same one to every caller after that.

Upstream v0.1.2 made `GetBlackMatrix` safe to share. This fork extends that to `GetBlackRow`, which
is the one the 1D readers use: `GlobalHistogramBinarizer` kept the luminance row and its histogram
on itself, so two goroutines reading rows of the same image wrote over each other's working memory.
Each read now borrows that memory from a pool.

**A `Reader` is not safe to share.** The readers keep working memory of their own between rows, so
give each goroutine a reader of its own. One `BinaryBitmap` and one reader per goroutine is the
combination to aim for: the bitmap holds the pixels and the black-and-white of the image, which is
the expensive part and worth sharing, while a reader is cheap to build.

```Go
// Read one image with several readers at once.
bmp, _ := gozxing.NewBinaryBitmapFromImage(img)

var wg sync.WaitGroup
found := make([][]*gozxing.Result, len(formats))
for i, newReader := range formats {
    wg.Add(1)
    go func(i int, reader gozxing.Reader) {
        defer wg.Done()
        found[i], _ = reader.Decode(bmp, nil) // one reader per goroutine, one shared bitmap
    }(i, newReader())
}
wg.Wait()
```

### Why This Matters

Creating a BinaryBitmap from an image can be computationally significant. For high-performance services, caching these preprocessed bitmaps can offer tangible benefits.

### Performance Benchmarks

The following benchmark demonstrates why caching BinaryBitmap instances is valuable:

```go
func BenchmarkCachingImpact(b *testing.B) {
    // Generate a 400x400 QR code
    key, _ := totp.Generate(totp.GenerateOpts{
        Issuer:      "BenchmarkApp",
        AccountName: "bench@example.com",
    })
    img, _ := key.Image(400, 400)
    
    b.Run("WithoutCaching", func(b *testing.B) {
        reader := qrcode.NewQRCodeReader()
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            // Create new BinaryBitmap each time (expensive!)
            bmp, _ := gozxing.NewBinaryBitmapFromImage(img)
            _, _ = reader.Decode(bmp, nil)
        }
    })
    
    b.Run("WithCaching", func(b *testing.B) {
        // Create BinaryBitmap once and reuse
        bmp, _ := gozxing.NewBinaryBitmapFromImage(img)
        reader := qrcode.NewQRCodeReader()
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            // Reuse the same BinaryBitmap (fast!)
            _, _ = reader.Decode(bmp, nil)
        }
    })
}
```

Results on Apple M1 Ultra:
```
BenchmarkCachingImpact/WithoutCaching-20     498    2392097 ns/op
BenchmarkCachingImpact/WithCaching-20       5311     188575 ns/op
```

This shows a **12.7x performance improvement** when caching BinaryBitmap instances.

For a pseudocode example of how you might leverage the added thread-safety of `gozxing`'s BinaryBitmap now:

```go
import (
    "bytes"
    "crypto/sha256"
    "encoding/hex"
    "image/jpeg"
    "sync"
    
    "github.com/teldio-operations/gozxing"
    "github.com/teldio-operations/gozxing/qrcode"
)

type QRResult struct {
    Text string
}

// High-performance QR service that caches preprocessed images
type QRService struct {
    cache sync.Map // image_hash -> *gozxing.BinaryBitmap
}

// ProcessQR handles QR detection for uploaded images.
// Without caching: Each request creates a new BinaryBitmap (1.7ms overhead)
// With caching: Reuse BinaryBitmap for duplicate images (12x faster)
func (s *QRService) ProcessQR(imageData []byte) (*QRResult, error) {
    hash := sha256.Sum256(imageData)
    hashStr := hex.EncodeToString(hash[:])
    
    // Check if we've already preprocessed this image
    if cached, ok := s.cache.Load(hashStr); ok {
        // Multiple goroutines may decode the same cached bitmap
        // This is now safe with v0.1.2+
        return s.decodeQR(cached.(*gozxing.BinaryBitmap))
    }
    
    // Preprocess new image (expensive: ~1.8ms for 400x400)
    img, _ := jpeg.Decode(bytes.NewReader(imageData))
    bmp, _ := gozxing.NewBinaryBitmapFromImage(img)
    
    // Cache for future requests
    s.cache.Store(hashStr, bmp)
    
    return s.decodeQR(bmp)
}

func (s *QRService) decodeQR(bmp *gozxing.BinaryBitmap) (*QRResult, error) {
    reader := qrcode.NewQRCodeReader()
    result, err := reader.Decode(bmp, nil) // Safe for concurrent use
    if err != nil {
        return nil, err
    }
    return &QRResult{Text: result.GetText()}, nil
}
```
