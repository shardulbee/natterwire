# Local Vaxis

`vaxis/` copies the complete Go module `go.rockorager.dev/vaxis@v0.17.1`
from the Go module proxy, retaining its license and module metadata.
Upstream commit: `1dea9475d7d75ec2c33a13777dcc96334b608b07`.
Module checksum: `h1:GE71riV9xp2rFDNqUDHU8JyoSOYHaC5G6zKte17oWbA=`.

Local changes are limited to `image.go` and `image_crop_test.go`: native-pixel
`KittyImage.DrawCrop(Window, image.Rectangle)`, crop-aware placement equality,
and publishing Kitty encoding readiness before its redraw event. Call Resize
once after NewKittyGraphic with the pre-scaled full image. Crop coordinates
refer to the resulting uploaded image, not the original image before Resize.
Cropping uses Kitty x/y/w/h without c/r; omission removes only the placement.
