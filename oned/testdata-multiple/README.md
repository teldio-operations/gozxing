# testdata-multiple

Images of several barcodes of one format, stacked down a white canvas.

`oned/multiple_reader_test.go` builds them when it runs. It reads every image of the matching
`oned/testdata` directory on its own, keeps the ones that hold a single barcode with a text no
earlier image already gave, draws them down one canvas, and reads that canvas back. The reader
has to report one result per barcode, from the top of the image down.

The `.png` files here are build output, not fixtures. `.gitignore` covers them, and deleting them
is safe. They stay on disk so that a failure is easy to look at.
