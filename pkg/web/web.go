package web

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
)

// StatusError is returned when the server answers with a non-200 status
type StatusError struct {
	Code int
}

func (e StatusError) Error() string {
	return fmt.Sprintf("http status %d", e.Code)
}

// IsNotFound reports whether err is an HTTP 404
func IsNotFound(err error) bool {
	var se StatusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}

// IsAuthError reports whether err is an HTTP 401 or 403
func IsAuthError(err error) bool {
	var se StatusError
	return errors.As(err, &se) && (se.Code == http.StatusUnauthorized || se.Code == http.StatusForbidden)
}

// Get fetches the contents of a URL
func Get(url string) (data []byte, err error) {
	return GetWithCheck(url, nil)
}

// GetWithCheck fetches the contents of a URL if the response passes a check
func GetWithCheck(url string, check func(*http.Response) error) (data []byte, err error) {
	res, err := http.Get(url)
	if err != nil {
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		err = StatusError{res.StatusCode}
		return
	}
	if check != nil {
		if err = check(res); err != nil {
			return
		}
	}
	return ioutil.ReadAll(res.Body)
}
