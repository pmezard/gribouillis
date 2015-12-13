package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dustin/go-humanize"
)

// fixImage decode input data as PNG, pad it with white at each borders and
// write it again as PNG on output write.
func fixImage(w io.Writer, r io.Reader, padding int) error {
	src, err := png.Decode(r)
	if err != nil {
		return err
	}
	srcRect := src.Bounds()
	dstRect := image.Rect(srcRect.Min.X-padding, srcRect.Min.Y-padding,
		srcRect.Max.X+padding, srcRect.Max.Y+padding)
	dst := image.NewRGBA(dstRect)
	white := color.RGBA{255, 255, 255, 255}
	for j := dstRect.Min.Y; j < dstRect.Max.Y; j++ {
		for i := dstRect.Min.X; i < dstRect.Max.X; i++ {
			if i >= srcRect.Min.X && i < srcRect.Max.X &&
				j >= srcRect.Min.Y && j < srcRect.Max.Y {
				dst.Set(i, j, src.At(i, j))
			} else {
				dst.Set(i, j, white)
			}
		}
	}
	return png.Encode(w, dst)
}

// save decode posted PNG and save it with a random name into imgDir. It returns
// a JSON response with the absolute path of the saved image.
func save(imgURL, imgDir string, maxImgSize int64, w http.ResponseWriter,
	r *http.Request) error {

	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%x", buf) + ".png"
	path := filepath.Join(imgDir, name)
	log.Printf("writing %s", path)
	fp, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fp.Close()

	err = fixImage(fp, &io.LimitedReader{
		R: r.Body,
		N: int64(maxImgSize),
	}, 20)
	if err != nil {
		return err
	}
	err = fp.Close()
	if err != nil {
		return err
	}
	rsp := struct {
		Path string `json:"path"`
	}{
		Path: imgURL + name,
	}
	w.Header().Set("Content-Type", "image/png")
	return json.NewEncoder(w).Encode(&rsp)
}

func gribouillis() error {
	flag.Usage = func() {
		fmt.Println(`Usage: gribouillis [OPTIONS]

gribouillis starts a web server on -http and exposes a "literallycanvas" web
drawing canvas on root URL. Saved images are serialized on disk in "images/"
relatively to the working directory and accessible with random URLs in "saved/"
subpath.

Use -base-url to set the web server base URL (useful when proxying).
`)
		os.Exit(1)
	}
	addr := flag.String("http", "localhost:5001", "HTTP host:port")
	baseURL := flag.String("base-url", "", "web server base URL")
	maxImgSizeStr := flag.String("max-image-size", "10MB", "maximum image size")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("no argument expected")
	}
	trimmed := strings.TrimRight(*baseURL, "/")
	baseURL = &trimmed
	maxImgSize, err := humanize.ParseBytes(*maxImgSizeStr)
	if err != nil {
		return err
	}

	imgDir := "images"
	imgURL := *baseURL + "/saved/"
	err = os.MkdirAll(imgDir, 0755)
	if err != nil {
		return err
	}
	http.Handle(imgURL, http.StripPrefix(imgURL,
		http.FileServer(http.Dir(imgDir))))
	http.HandleFunc(*baseURL+"/save/", func(w http.ResponseWriter, r *http.Request) {
		err := save(imgURL, imgDir, int64(maxImgSize), w, r)
		if err != nil {
			log.Printf("save error: %s", err)
			w.WriteHeader(500)
			w.Write([]byte(fmt.Sprintf("could not save image: %s", err)))
		}
	})
	http.Handle(*baseURL+"/", http.StripPrefix(*baseURL+"/",
		http.FileServer(http.Dir("literallycanvas"))))
	return http.ListenAndServe(*addr, nil)
}

func main() {
	err := gribouillis()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}
