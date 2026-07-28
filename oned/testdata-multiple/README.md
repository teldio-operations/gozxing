# testdata-multiple

Images of several barcodes of one format on one canvas.

`oned/multiple_reader_test.go` builds them when it runs. It reads every image of the matching
`oned/testdata` directory on its own, keeps the ones that hold a single barcode with a text no
earlier image already gave, and lays those out two ways:

- `<format>.png` stacks them in a column. Every row of the canvas crosses one barcode.
- `<format>-grid.png` puts four of them in a square: top-left, top-right, bottom-left,
  bottom-right. Every row of the canvas crosses two barcodes, so the reader has to carry on along
  a row after it has already read a barcode from it.

The reader has to report one result per barcode. The source images butt up against each other,
with no gap between them and no margin around them, because laying pure white next to a photo of
a tinted label skews the black point that `GetBlackRow` estimates from that row.

The `.png` files here are build output, not fixtures. `.gitignore` covers them, and deleting them
is safe. They stay on disk so that a failure is easy to look at.
