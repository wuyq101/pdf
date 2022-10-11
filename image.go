package pdf

import (
	"bytes"
	"image"
	"image/jpeg"
	"log"

	"golang.org/x/image/tiff"
)

func CompressImage(data []byte) []byte {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		log.Default().Printf("decode image err: %v", err)
		return data
	}
	log.Default().Printf("image format: %s", format)
	buf := bytes.Buffer{}
	err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 5})
	if err != nil {
		return data
	}
	if buf.Len() > len(data) {
		return data
	}
	return buf.Bytes()
}

func CompressTIFFImage(data []byte) []byte {
	img, err := tiff.Decode(bytes.NewReader(data))
	if err != nil {
		log.Default().Printf("decode image err: %v", err)
		return data
	}

	buf := bytes.Buffer{}
	err = tiff.Encode(&buf, img, &tiff.Options{Compression: tiff.LZW})
	if err != nil {
		log.Default().Printf("decode image err: %v", err)
		return data
	}

	log.Default().Printf("compress tiff %d ---> %d", len(data), buf.Len())
	return buf.Bytes()
}
