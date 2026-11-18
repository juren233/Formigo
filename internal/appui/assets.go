package appui

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/mascot.png
var mascotPNG []byte

//go:embed assets/avatar.png
var avatarPNG []byte

var (
	// MascotBitmap is the transparent-background Go Gopher converter illustration (520x350).
	MascotBitmap = mustDecodeBitmap(mascotPNG)

	// AvatarBitmap is the square Gopher brand avatar badge (192x192).
	AvatarBitmap = mustDecodeBitmap(avatarPNG)
)

func mustDecodeBitmap(data []byte) *ui.Bitmap {
	bmp, err := ui.DecodeBitmap(data)
	if err != nil {
		panic("formigo: failed to decode embedded brand bitmap: " + err.Error())
	}
	return bmp
}
