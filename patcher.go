package main

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/klauspost/compress/s2"
	"github.com/vbauerster/mpb"
	"github.com/vbauerster/mpb/decor"
	"golang.org/x/crypto/blake2b"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const NumCDNs = 5
const DefaultForceDownload = false

var installDirectory, _ = os.Getwd()
var progressBarManager *mpb.Progress
var localVersionDB *VersionFile
var onlineCDNs []int
var onlineServers int

type File struct {
	PathLen      uint32
	Path         string
	HashLen      uint32
	Hash         string
	LastModified int64
}

type VersionFile struct {
	Padding       [16]byte
	NumberOfFiles uint32
	Files         []File
}

func checkCDNStatus() ([]int, int) {
	var online []int
	for i := 0; i < NumCDNs; i++ {
		formattedUrl := fmt.Sprintf("https://cdn%v.burningsw.to/", i)
		resp, err := http.Head(formattedUrl)
		if err == nil {
			if resp.StatusCode == 200 {
				online = append(online, i)
			}
		}
	}
	return online, len(online)
}

func fetchVersionFile(local bool) (*VersionFile, error) {
	if local {
		log.Println("Loading version file from local storage.")
		file, err := os.Open("version.bin")
		if err != nil {
			return nil, err
		}
		dec := gob.NewDecoder(file)
		var versionFile VersionFile
		err = dec.Decode(&versionFile)
		return &versionFile, err
	} else {
		log.Println("Loading version file from remote cdn.")
		data, err := getFile(fmt.Sprintf("https://cdn%v.burningsw.to/version.bin", onlineCDNs[0]))
		if err != nil {
			return nil, err
		}
		for i := range data {
			data[i] ^= byte(i%0xFF + 0x69)
		}
		return unmarshalVersionFile(bytes.NewBuffer(data)), nil
	}
}

func unmarshalVersionFile(buffer *bytes.Buffer) *VersionFile {
	versionFile := &VersionFile{}

	_ = binary.Read(buffer, binary.LittleEndian, &versionFile.Padding)

	_ = binary.Read(buffer, binary.LittleEndian, &versionFile.NumberOfFiles)
	versionFile.Files = make([]File, versionFile.NumberOfFiles)

	for i := range versionFile.Files {
		file := &versionFile.Files[i]

		_ = binary.Read(buffer, binary.LittleEndian, &file.PathLen)
		strBuffer := make([]byte, file.PathLen)

		_ = binary.Read(buffer, binary.LittleEndian, &strBuffer)
		file.Path = string(strBuffer)

		_ = binary.Read(buffer, binary.LittleEndian, &file.HashLen)
		hashBuffer := make([]byte, file.HashLen)

		_ = binary.Read(buffer, binary.LittleEndian, &hashBuffer)
		file.Hash = string(hashBuffer)

		_ = binary.Read(buffer, binary.LittleEndian, &file.LastModified)
	}

	return versionFile
}

func diffVersionFile(cdn *VersionFile, local *VersionFile) []File {
	var toDownload []File

	for _, cdnFile := range cdn.Files {
		localFile, index := local.findFileByName(cdnFile.Path)
		if localFile == nil {
			// need to download
			log.Printf("File does not exist or is read only %v\n", cdnFile.Path)
			toDownload = append(toDownload, cdnFile)
		} else if f, err := os.Open(localFile.Path); err == nil {
			if fi, err := f.Stat(); err == nil && fi.Mode() == os.FileMode(0444) {
				log.Println(cdnFile.Path, "file is read only")
				continue
			} else if err == nil && fi.ModTime().Unix() != localFile.LastModified {
				// Modified local file without read-only permission, update
				log.Printf("Different mod time %s = %v != %v\n", cdnFile.Path, localFile.LastModified, fi.ModTime().Unix())
				local.Files = removeFile(local.Files, index)
				toDownload = append(toDownload, cdnFile)
			} else if localFile.Hash != cdnFile.Hash {
				// need up update
				log.Printf("Different file hash %s = %v != %v\n", cdnFile.Path, localFile.Hash, cdnFile.Hash)
				local.Files = removeFile(local.Files, index)
				toDownload = append(toDownload, cdnFile)
			} else {
				//log.Println("ok")
			}
		}
	}
	local.NumberOfFiles = uint32(len(local.Files))

	return toDownload
}

func verifyFiles(files []File) []File {
	var toDownload []File

	for _, file := range files {
		fileName := file.Path

		hasher, _ := blake2b.New256(nil)
		fmt.Printf("Checking %s: ", fileName)
		localFile, err := os.Open(filepath.Join(installDirectory, fileName))

		if err != nil {
			println("Need to download.")
			toDownload = append(toDownload, file)
			continue
		}

		if _, err := io.Copy(hasher, localFile); err != nil {
			println("Need to download.")
			toDownload = append(toDownload, file)
			continue
		}

		hashBytes := hasher.Sum(nil)
		hash := hex.EncodeToString(hashBytes[:])

		localVersionDB.Files = append(localVersionDB.Files, File{
			Path:         fileName,
			Hash:         hash,
			LastModified: file.LastModified,
		})

		fi, err := localFile.Stat()
		if err == nil && fi.Mode() == os.FileMode(0444) {
			println("File is custom (read-only), skipping.")
			continue
		}
		localFile.Close()

		if hash != file.Hash {
			println("Need to download.")
			toDownload = append(toDownload, file)
			continue
		}

		lm := time.Unix(file.LastModified, 0)
		err = os.Chtimes(file.Path, lm, lm)
		println("OK.")
	}

	return toDownload
}

func downloadFiles(toDownload []File, numWorkers int) {
	progressBarManager = mpb.New()
	var wg sync.WaitGroup
	wg.Add(len(toDownload))

	jobs := make(chan File, len(toDownload))

	for w := 0; w < numWorkers; w++ {
		go worker(w, jobs, &wg)
	}

	for _, file := range toDownload {
		jobs <- file
	}

	defer close(jobs)

	wg.Wait()
	progressBarManager.Wait()
}

func worker(id int, jobs <-chan File, wg *sync.WaitGroup) {
	for j := range jobs {
		formattedUrl := fmt.Sprintf("https://cdn%v.burningsw.to/%s", onlineCDNs[id%onlineServers], j.Path)
		formattedUrl = strings.ReplaceAll(formattedUrl, "\\", "/")
		force := DefaultForceDownload
		for {
			err := downloadFile(j, formattedUrl, wg, force)
			if err != nil {
				if force {
					println("Download for", formattedUrl, "failed again, check manually.")
					wg.Done()
					break
				}
				// force download fresh
				log.Println(err, " (", formattedUrl, "), Retrying.")
				force = true
				continue
			} else {
				localVersionDB.Files = append(localVersionDB.Files, j)
				if err := localVersionDB.save(); err != nil {
					log.Panic(err)
				}
			}
			break
		}
	}
}

func downloadFile(file File, url string, wg *sync.WaitGroup, force bool) error {
	filename := file.Path
	// Create the file, but give it a tmp file extension, this means we won't overwrite a
	// file until it's downloaded, but we'll remove the tmp extension once downloaded.
	info, err := os.Stat(filename + ".tmp")

	var currPosition int64
	var out *os.File
	x, dlerr := http.NewRequest("GET", url, nil)
	if dlerr != nil {
		log.Panic(dlerr)
		return dlerr
	}

	if !force && err == nil {
		currPosition = info.Size()
		/* DownloadNewestFileCheck
		if info.ModTime().Unix() != file.LastModified { // Doesn't work because golang changes file modtime on write
			fmt.Printf("File %s modification time has changed, assuming new version (%v vs %v)\n", info.Name(), info.ModTime().Format("1/2/2006 3:04 PM"), time.Unix(file.LastModified, 0).Format("1/2/2006 3:04 PM"))
			currPosition = 0
		} else {
		*/
		x.Header.Add("Range", fmt.Sprintf("bytes=%v-", currPosition))
		fmt.Printf("Resuming %s from byte position %v.\n", filename, humanize.Bytes(uint64(currPosition)))
		//}
		out, err = os.OpenFile(filename+".tmp", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0777)
		if err != nil {
			return err
		}
	} else {
		out, err = os.Create(filename + ".tmp")
	}

	// Get the data
	client := &http.Client{}
	resp, err := client.Do(x)
	if err != nil {
		return err
	}

	// Create our progress reporter and pass it to be used alongside our writer
	bar := progressBarManager.AddBar(currPosition+resp.ContentLength,
		mpb.PrependDecorators(
			decor.Name(filename+" > "),
			decor.CountersKibiByte("% .2f / % .2f"),
		),
		mpb.AppendDecorators(
			decor.OnComplete(
				decor.EwmaETA(decor.ET_STYLE_GO, 60),
				"done",
			),
			decor.Name(" @ "),
			decor.EwmaSpeed(decor.UnitKiB, "% .2f", 60),
		),
	)

	defer bar.Abort(true) // Remove the bar when it's done downloading to clean up the console
	proxyReader := bar.ProxyReader(resp.Body)
	if currPosition > 0 {
		bar.IncrInt64(currPosition)
	}
	if _, err = io.Copy(out, proxyReader); err != nil {
		//log.Panic(err)
		return err
	}
	defer proxyReader.Close() // Close file handles on exit
	defer resp.Body.Close()

	decompress, err := os.Create(filename)
	if err != nil {
		//log.Panic(err)
		return err
	}

	_ = out.Close()
	out, err = os.Open(filename + ".tmp") // reopen for reading
	if err != nil {
		//log.Panic(err)
		return err
	}

	if _, err = io.Copy(decompress, s2.NewReader(out)); err != nil { // Decompress the data using s2d
		return err
	}
	_ = out.Close()
	_ = os.Remove(filename + ".tmp")

	lm := time.Unix(file.LastModified, 0)
	err = os.Chtimes(file.Path, lm, lm)

	wg.Done()
	return nil
}

func getFile(path string) ([]byte, error) {
	resp, err := http.Get(path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	return body, err
}

func (versionFile *VersionFile) save() error {
	versionFile.NumberOfFiles = uint32(len(versionFile.Files))
	if out, err := os.OpenFile("version.bin", os.O_CREATE|os.O_WRONLY, 0777); err != nil {
		return err
	} else {
		enc := gob.NewEncoder(out)
		return enc.Encode(versionFile)
	}
}

func (versionFile *VersionFile) findFileByName(path string) (*File, int) {
	for i, f := range versionFile.Files {
		if f.Path == path {
			return &f, i
		}
	}

	return nil, -1
}

func removeFile(s []File, i int) []File {
	s[len(s)-1], s[i] = s[i], s[len(s)-1]
	return s[:len(s)-1]
}
