//go:build ignore

package main

import (
	"log"
	"os"

	"github.com/wuyq101/pdf"
)

func init() {
	// set log format
	flags := log.Default().Flags() | log.Lshortfile
	log.Default().SetFlags(flags)
}

func main() {
	/*
		f, err := pdf.ReadFromFile("./test-data/test.pdf")
		if err != nil {
			panic(err)
		}
		log.Default().Printf("read pdf file: %v\n", f.Header)
		//	f.ExportJPEG()
		f.SaveFile("./test-data/cf-test.pdf", true)
	*/
	f, err := pdf.ReadFromFile("./test-data/test1.pdf")
	if err != nil {
		panic(err)
	}
	log.Default().Printf("read pdf file: %v\n", f.Header)
	//	f.ExportJPEG()
	f.SaveFile("./test-data/cf-test1.pdf", true)
	testTIFF()
}

func testTIFF() {
	buf, _ := os.ReadFile("./test-data/golang-gopher.tiff")
	log.Default().Printf("first 200 %v", buf[:500])
}
