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
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
)

type File struct {
	Name string
	Size int64
}

// LimitedDir tracks child files of a directory and ensure there are at most
// maxCount of them or the total size is less than maxSize. Otherwise, oldest
// one are deleted until the conditions are matched. LimitedDir can be used
// concurrently.
//
// Known limitations:
// - Adding an existing file count as a new one. This is not a problem in
//   gribouillis as saved drawings always carry new name, and if they do not, the
//   LimitedDir will be a bit more punitive.
// - Empty files are tolerated. Again, not a problem since gribouillis store
//   valid PNG files.
type LimitedDir struct {
	path     string
	maxSize  int64
	maxCount int
	lock     sync.Mutex
	files    []File
	size     int64
}

type sortedFiles []os.FileInfo

func (s sortedFiles) Len() int {
	return len(s)
}

func (s sortedFiles) Less(i, j int) bool {
	ti := s[i].ModTime()
	tj := s[j].ModTime()
	return ti != tj && tj.After(ti)
}

func (s sortedFiles) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// OpenLimitedDir returns a LimitedDir initialized on supplied directory.
func OpenLimitedDir(path string, maxSize int64, maxCount int) (*LimitedDir, error) {
	err := os.MkdirAll(path, 755)
	if err != nil {
		return nil, err
	}
	entries, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}
	sort.Sort(sortedFiles(entries))
	files := make([]File, len(entries))
	total := int64(0)
	for i, e := range entries {
		if !e.Mode().IsRegular() {
			continue
		}
		files[i] = File{
			Name: e.Name(),
			Size: e.Size(),
		}
		total += files[i].Size
	}
	d := &LimitedDir{
		path:     path,
		maxCount: maxCount,
		files:    files,
		size:     total,
		maxSize:  maxSize,
	}
	err = d.shrink()
	if err != nil {
		return nil, err
	}
	return d, err
}

func (d *LimitedDir) Path() string {
	return d.path
}

func (d *LimitedDir) shrink() error {
	for (d.size > d.maxSize && len(d.files) > 0) || len(d.files) > d.maxCount {
		f := d.files[0]
		p := filepath.Join(d.path, f.Name)
		log.Printf("removing %s", f.Name)
		err := os.Remove(p)
		if err != nil && !os.IsNotExist(err) {
			return err
		} else if err == nil {
			d.size -= f.Size
		}
		d.files = d.files[1:]
	}
	return nil
}

// Add registers a new file in the LimitedDir and applies the maxCount/maxSize
// policy. Note that adding an existing files works like adding a new one.
func (d *LimitedDir) Add(name string) error {
	path := filepath.Join(d.path, name)
	st, err := os.Stat(path)
	if err != nil {
		return err
	}

	d.lock.Lock()
	defer d.lock.Unlock()
	d.files = append(d.files, File{
		Name: name,
		Size: st.Size(),
	})
	d.size += st.Size()
	return d.shrink()
}

// List returns the list of tracked files in deletion order.
func (d *LimitedDir) List() []string {
	d.lock.Lock()
	defer d.lock.Unlock()
	names := []string{}
	for _, f := range d.files {
		names = append(names, f.Name)
	}
	return names
}

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
func save(imgURL string, imgDir *LimitedDir, maxImgSize int64, w http.ResponseWriter,
	r *http.Request) error {

	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%x", buf) + ".png"
	path := filepath.Join(imgDir.Path(), name)
	log.Printf("writing %s", path)
	fp, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if fp != nil {
			fp.Close()
			os.Remove(path)
		}
	}()

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
	fp = nil
	err = imgDir.Add(name)
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
		flag.PrintDefaults()
		os.Exit(1)
	}
	addr := flag.String("http", "localhost:5001", "HTTP host:port")
	baseURL := flag.String("base-url", "", "web server base URL")
	maxImgSizeStr := flag.String("max-image-size", "10MB", "maximum image size")
	minDelayStr := flag.String("min-delay", "5s", "minimum delay between two records")
	maxSizeStr := flag.String("max-size", "50MB",
		"maximum combined size of saved drawings")
	maxCount := flag.Int("max-count", 500, "maximum number of saved drawings")
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
	maxSize, err := humanize.ParseBytes(*maxSizeStr)
	if err != nil {
		return err
	}
	minDelay, err := time.ParseDuration(*minDelayStr)
	if err != nil {
		return err
	}
	lastTimeMutex := sync.Mutex{}
	lastTime := time.Now()

	imgURL := *baseURL + "/saved/"
	imgDir, err := OpenLimitedDir("images", int64(maxSize), *maxCount)
	if err != nil {
		return err
	}
	http.Handle(imgURL, http.StripPrefix(imgURL,
		http.FileServer(http.Dir(imgDir.Path()))))
	http.HandleFunc(*baseURL+"/save/", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		lastTimeMutex.Lock()
		last := lastTime
		lastTimeMutex.Unlock()
		if now.Sub(last) < minDelay {
			log.Printf("rate limited")
			w.WriteHeader(429)
			w.Write([]byte("rate limited"))
			return
		}
		lastTimeMutex.Lock()
		lastTime = now
		lastTimeMutex.Unlock()

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
