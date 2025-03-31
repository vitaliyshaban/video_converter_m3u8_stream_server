package method

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/gorilla/websocket"
)

type ErrorSW struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

func ErrorMessageWS(conn *websocket.Conn, err error, message string) {
	errorRes := ErrorSW{
		Success: false,
		Message: message,
		Error:   err.Error(),
	}

	bytes, err := json.Marshal(errorRes)
	if err != nil {
		log.Printf("Error sending progress message: %v", err)
		return
	}

	err = conn.WriteMessage(websocket.TextMessage, bytes)
	if err != nil {
		log.Printf("Failed to send message: %v\n", err)
		return
	}
	// conn.Close()
}

func ListFilesInDirectory(directoryPath string) ([]string, error) {
	var files []string
	err := filepath.Walk(directoryPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func GenerateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func RemoveLocalFile(outputFileName string) error {
	if err := os.Remove(outputFileName); err != nil {
		return err
	}
	// fmt.Printf("Файл %v успешно удален с локального диска\n", outputFileName)
	return nil
}

// GenerateKey генерирует уникальный ключ указанного размера в байтах
func GenerateKey(size int) (string, error) {
	// Создаем срез байтов указанного размера
	bytes := make([]byte, size)

	// Заполняем срез случайными байтами
	_, err := rand.Read(bytes)
	if err != nil {
		return "", fmt.Errorf("error generating random bytes: %v", err)
	}

	// Преобразуем байты в строку в формате hex
	key := hex.EncodeToString(bytes)

	return key, nil
}
