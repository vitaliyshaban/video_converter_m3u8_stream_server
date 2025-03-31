package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
	"m3u8.com/src/lib/ai"
	ffmpeg "m3u8.com/src/lib/ffmpeg"
	fb "m3u8.com/src/lib/firebase"
	method "m3u8.com/src/lib/methods"
)

type Video struct {
	Hash        string   `json:"hash"`
	Name        string   `json:"name"`
	Resolutions []string `json:"resolutions"`
	Id          string   `json:"id"`
	Timestamp   string   `json:"timestamp"`
}

type Message struct {
	VideoList []Video `json:"videoList"`
}

// Структура для представления ответа JSON
type OutputData struct {
	Message string `json:"message"`
	Status  int    `json:"status"`
	Url     string `json:"url"`
}

func creatVideoSegmentsHandle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Проверка метода запроса
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var m Message

	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}
	err := json.NewDecoder(r.Body).Decode(&m)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	segmentsDir := "segments"

	for _, video := range m.VideoList {
		segments := map[string]string{}
		folderSegment := fmt.Sprintf("%v/%v", segmentsDir, video.Hash)
		if err := fb.DownloadVideo(context.Background(), video.Name); err != nil {
			fmt.Printf("Failed to download video file:  %v", err)
		}

		if err := os.MkdirAll(folderSegment, 0755); err != nil {
			fmt.Printf("Ошибка при создании папки для сегментов: %v", err)
		}

		_, err := ffmpeg.CreatePoster(video.Name, folderSegment, video.Timestamp, video.Hash)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		for _, resolution := range video.Resolutions {
			resX := strings.Split(resolution, "x")[1]
			segmentOutput := fmt.Sprintf("%v/%v_%v_", folderSegment, video.Hash, resX)
			segments[resolution] = fmt.Sprintf("%v/%v_%v_.m3u8?alt=media", folderSegment, video.Hash, resX)
			if err := ffmpeg.CreateSegments(video.Name, segmentOutput, resolution); err != nil {
				fmt.Printf("Ошибка при создании сегментов %vp: %v\n", resX, err)
			}
		}
		if err := method.RemoveLocalFile(video.Name); err != nil {
			fmt.Printf("Ошибка удаления видео %v:  %v\n", video.Name, err)
		}

		manifest := fmt.Sprintf("%v/%v.m3u8", folderSegment, video.Hash)
		if err := ffmpeg.CreateMasterM3U8(manifest, segments); err != nil {
			fmt.Println("Ошибка при создании файла манифеста:", err)
		}

		// Загрузка сегментов обратно в Google Cloud Storage
		files, err := method.ListFilesInDirectory(folderSegment)
		if err != nil {
			fmt.Printf("Error listing files: %v\n", err)
			return
		}

		if _, err := fb.UploadFilesToFireStorage(context.Background(), files, folderSegment); err != nil {
			fmt.Println("Ошибка при загрузке в Firestorage:", err)
		}

		// Запись метаданных в Firestore
		url := "/segments%2F" + video.Hash + "%2F" + video.Hash + ".m3u8?alt=media"
		posterUrl := "/segments%2F" + video.Hash + "%2F" + video.Hash + ".jpg?alt=media"
		metadata := map[string]interface{}{
			"segments": true,
			"url":      url,
			"poster":   posterUrl,
		}

		err = fb.UpdateVideoMetadata(context.Background(), metadata, video.Id)
		if err != nil {
			fmt.Printf("Ошибка записи метаданных в Firestore: %v", err)
		}

		// Возврат успешного ответа
		responseMessage := fmt.Sprintf("Видео, %v! Хеш: %v", video.Name, video.Hash)
		outputData := OutputData{
			Message: responseMessage,
			Status:  http.StatusOK,
			Url:     url,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// bytes, err := json.Marshal(outputData)
		// if err != nil {
		// 	log.Printf("Error sending progress message: %v", err)
		// 	break
		// }
		// w.Write(bytes)
		// w.Header().Set("Content-Type", "application/json")
		// w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(outputData)

	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type VideoRequest struct {
	Resolutions []string `json:"resolutions"`
}

type ProgressData = ffmpeg.ProgressData

func convertVideoHandler(w http.ResponseWriter, r *http.Request) {

	idToken := r.URL.Query().Get("token")
	if idToken == "" {
		http.Error(w, "Missing token", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка апгрейда соединения:", err)
		return
	}
	defer conn.Close()

	claims, err := fb.IsAuthAdmin(context.Background(), idToken)
	if err != nil {
		method.ErrorMessageWS(conn, err, "Ошибка авторизации")
		return
	}

	if len(claims) == 0 {
		method.ErrorMessageWS(conn, err, "Ошибка авторизации")
		return
	}

	_, message, err := conn.ReadMessage()
	if err != nil {
		// log.Println("Ошибка чтения сообщения из WebSocket:", err)
		method.ErrorMessageWS(conn, err, "Ошибка чтения сообщения из WebSocket:")
	}

	var req VideoRequest
	err = json.Unmarshal(message, &req)
	if err != nil {
		method.ErrorMessageWS(conn, err, "Ошибка декодирования JSON:")
		// log.Println("Ошибка декодирования JSON:", err)
	}

	// Create a temporary file for the uploaded video
	tempFile, err := os.CreateTemp("", "video_*.mp4")
	if err != nil {
		// log.Println("Ошибка создания временного файла:", err)
		method.ErrorMessageWS(conn, err, "Ошибка создания временного файла:")
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	// Read video data from WebSocket and write to the temp file
	for {
		_, videoData, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				break
			}
			log.Println("Ошибка чтения видео данных из WebSocket:", err)
			return
		}

		if len(videoData) == 0 {
			// Empty message indicates end of transmission
			break
		}

		_, err = tempFile.Write(videoData)
		if err != nil {
			log.Println("Ошибка записи видео данных во временный файл:", err)
			return
		}
	}

	// Close temp file to ensure all data is flushed to disk
	err = tempFile.Close()
	if err != nil {
		log.Println("Ошибка закрытия временного файла:", err)
		return
	}

	// Generate hash of the uploaded video
	hash, err := method.GenerateFileHash(tempFile.Name())
	if err != nil {
		log.Println("ошибка при генерации хеша файла:", err)
		return
	}
	outputDirName := "output"
	storageDirName := "videos"

	// Check if a file with the same hash already exists
	for n, resolution := range req.Resolutions {

		parts := strings.Split(resolution, "x")
		if len(parts) != 2 {
			return
		}

		width := parts[0]
		height := parts[1]

		outputFileName := fmt.Sprintf("%s/%s_%s_%s.mp4", outputDirName, hash, width, height)
		objectName := fmt.Sprintf("%s/%s_%s_%s.mp4", storageDirName, hash, width, height)
		outputFilesName := []string{outputFileName}
		if err := os.MkdirAll(outputDirName, 0755); err != nil {
			log.Fatal("Ошибка при создании папки для сегментов:", err)
		}

		// проверяем загружен ли файл в firestorage
		exists, err := fb.FileExistsInFirestorage(objectName)
		if err != nil {
			fmt.Printf("Error checking file existence: %v\n", err)
		} else if exists {
			fmt.Printf("Файл уже загружен в Firestorage: %s\n", outputFileName)
			result := method.ErrorSW{
				Success: false,
				Error:   "File was load",
				Message: fmt.Sprintf("Файл уже загружен в Firestorage: %s", outputFileName),
			}

			bytes, err := json.Marshal(result)
			if err != nil {
				log.Printf("Error sending progress message: %v", err)
				break
			}

			conn.WriteMessage(websocket.TextMessage, bytes)
			return
		} else {
			fmt.Printf("File %v does not exist in the bucket.\n", objectName)
		}

		// проверяем загружен ли файл локально
		log.Println(outputFileName)
		if _, err := os.Stat(outputFileName); err == nil {
			// File already exists
			log.Println("Файл уже загружен на сервер:", outputFileName)
			result := ProgressData{
				Success:          false,
				Error:            fmt.Sprintf("Файл уже загружен на сервер: %s", outputFileName),
				TotalResolutions: n,
				Size:             parts,
			}

			bytes, err := json.Marshal(result)
			if err != nil {
				log.Printf("Error sending progress message: %v", err)
				break
			}
			conn.WriteMessage(websocket.TextMessage, bytes)

			// значит не будем конвертировать и грузим в Firestorage
			if _, err := fb.UploadFilesToFireStorage(context.Background(), outputFilesName, storageDirName); err != nil {
				fmt.Printf("Failed to upload file: %v\n", err)
			} else {
				fmt.Println("Файл загружен в Firestorage")
			}
			return
		}

		// Convert the video
		err = ffmpeg.ConvertVideo(tempFile.Name(), req.Resolutions, conn)
		if err != nil {
			log.Println("Ошибка конвертации видео:", err)
			return
		}

		// Создание метаданных
		metadata := fb.VideoMetadata{
			Title:    "",
			Name:     fmt.Sprintf("%s_%s_%s", hash, width, height),
			Hash:     hash,
			Extname:  "mp4",
			Storage:  false,
			Segments: false,
			Poster:   "",
			Url:      "",
		}

		_, err = fb.UploadFilesToFireStorage(context.Background(), outputFilesName, storageDirName)
		if err != nil {
			fmt.Printf("Failed to upload file: %v\n", err)
		} else {
			metadata.Storage = true
			fmt.Println("Файл загружен в Firestorage")
		}

		// Запись метаданных в Firestore
		err = fb.SaveVideoMetadata(context.Background(), metadata, "videos")
		if err != nil {
			fmt.Printf("Ошибка записи метаданных в Firestore: %v", err)
		}
	}

}

type PosterData struct {
	Video     string `json:"video"`
	Timestamp string `json:"timestamp"`
}

func creatPosterHandle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Проверка метода запроса
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var m PosterData

	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}
	err := json.NewDecoder(r.Body).Decode(&m)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	fmt.Println(m)

	if err := fb.DownloadVideo(context.Background(), m.Video); err != nil {
		fmt.Printf("Failed to download video file:  %v", err)
	}
	outputDir := "posters"
	res, err := ffmpeg.CreatePoster(m.Video, outputDir, m.Timestamp, "poster")
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	println(res)
	// Возврат успешного ответа
	responseMessage := fmt.Sprintf("Видео, %v! Time: %v", m.Video, m.Timestamp)
	responseData := OutputData{
		Message: responseMessage,
		Status:  http.StatusOK,
		Url:     m.Video,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(responseData)
}

type Chapters struct {
	Chapters []fb.Chapter `json:"chapters"`
	Id       string       `json:"id"`
	Hash     string       `json:"hash"`
}

func createVTTHandle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Проверка метода запроса
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var fl Chapters

	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}
	err := json.NewDecoder(r.Body).Decode(&fl)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	fmt.Println(fl)

	// Создание VTT файла
	folderDir := fmt.Sprintf("segments/%v", fl.Hash)
	outputPath := fmt.Sprintf("%v/%v.vtt", folderDir, fl.Hash)
	err = createVTTFile(fl.Chapters, outputPath)
	if err != nil {
		fmt.Printf("Error creating VTT file: %v\n", err)
		return
	}

	// Создание метаданных
	metadata := map[string]interface{}{
		"chapters": fl.Chapters,
	}
	// Запись метаданных в Firestore
	err = fb.UpdateVideoMetadata(context.Background(), metadata, fl.Id)
	if err != nil {
		fmt.Printf("Ошибка записи метаданных в Firestore: %v", err)
	}

	files := []string{outputPath}

	_, err = fb.UploadFilesToFireStorage(context.Background(), files, folderDir)
	if err != nil {
		fmt.Println("Ошибка при загрузке в Firestorage:", err)
	}

	// println(res)
	// Возврат успешного ответа
	responseMessage := fmt.Sprintf("good %v", 0)
	responseData := OutputData{
		Message: responseMessage,
		Status:  http.StatusOK,
		Url:     "",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(responseData)
}

func createVTTFile(chapters []fb.Chapter, outputPath string) error {
	// Создание и открытие файла
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Запись заголовка VTT
	_, err = file.WriteString("WEBVTT\n\n")
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	// Запись субтитров
	for _, chapters := range chapters {
		_, err := file.WriteString(fmt.Sprintf("%s --> %s\n%s\n\n", chapters.Start, chapters.End, chapters.Text))
		if err != nil {
			return fmt.Errorf("failed to write subtitle to file: %w", err)
		}
	}

	return nil
}

type Transcription struct {
	Data string `json:"data"`
}

func transcriptionHandlerWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка апгрейда соединения:", err)
		return
	}
	defer conn.Close()

	_, message, err := conn.ReadMessage()
	if err != nil {
		method.ErrorMessageWS(conn, err, "Ошибка чтения сообщения из WebSocket:")
	}

	var req Transcription
	err = json.Unmarshal(message, &req)
	if err != nil {
		method.ErrorMessageWS(conn, err, "Ошибка декодирования JSON:")
	}

	err = ai.GetSubtitlesJSONws(conn, "audios/output_audio.wav")
	if err != nil {
		fmt.Println("failed:", err)
	}
}

type TranscriptionRes struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func transcriptionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Проверка метода запроса
	if r.Method != http.MethodPost {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	var m Transcription

	if r.Body == nil {
		http.Error(w, "Please send a request body", 400)
		return
	}
	err := json.NewDecoder(r.Body).Decode(&m)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	fmt.Println(m)

	err = ai.GetSubtitlesJSON()
	if err != nil {
		// http.Error(w, err.Error(), 400)
		fmt.Println("failed:", err)
	}

	result := TranscriptionRes{
		Success: true,
		Message: "complited",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// bytes, err := json.Marshal(outputData)
	// if err != nil {
	// 	log.Printf("Error sending progress message: %v", err)
	// 	break
	// }
	// w.Write(bytes)
	// w.Header().Set("Content-Type", "application/json")
	// w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func preprocessVideoHandler(w http.ResponseWriter, r *http.Request) {

	accaunt, err := method.GenerateKey(16)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Ошибка апгрейда соединения:", err)
		return
	}
	defer conn.Close()

	// Create a temporary file for the uploaded video
	tempFile, err := os.CreateTemp("", "video_*.mp4")
	if err != nil {
		// log.Println("Ошибка создания временного файла:", err)
		method.ErrorMessageWS(conn, err, "Ошибка создания временного файла:")
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	for {
		_, videoData, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				break
			}
			log.Println("Ошибка чтения видео данных из WebSocket:", err)
			return
		}

		if len(videoData) == 0 {
			// Empty message indicates end of transmission
			break
		}

		_, err = tempFile.Write(videoData)
		if err != nil {
			log.Println("Ошибка записи видео данных во временный файл:", err)
			return
		}
	}
	// Close temp file to ensure all data is flushed to disk
	err = tempFile.Close()
	if err != nil {
		log.Println("Ошибка закрытия временного файла:", err)
		return
	}

	// Creat thumb
	folder, err := method.GenerateKey(16)
	if err != nil {
		fmt.Println("Error:", err)
	}
	pathThumbs := "thumbs"
	pathFull := fmt.Sprintf("%s/%s", pathThumbs, folder)
	if err := os.MkdirAll(pathFull, 0755); err != nil {
		fmt.Printf("Ошибка при создании папки для сегментов: %v", err)
	}

	err = ffmpeg.CreatFramesVideo(tempFile.Name(), conn, pathFull)
	if err != nil {
		log.Println("Ошибка конвертации видео:", err)
		return
	}

	log.Println(accaunt)

	files, err := method.ListFilesInDirectory(pathFull)
	if err != nil {
		fmt.Printf("Error listing files: %v\n", err)
		return
	}
	// files
	folderStorageThumbs := fmt.Sprintf("%s/%s/%s/%s", "creator", accaunt, folder, pathThumbs)
	filesPath, err := fb.UploadFilesToFireStorage(context.Background(), files, folderStorageThumbs)
	if err != nil {
		fmt.Println("Ошибка при загрузке в Firestorage:", err)
	}

	hash, err := method.GenerateFileHash(tempFile.Name())
	if err != nil {
		log.Println("ошибка при генерации хеша файла:", err)
		return
	}

	videos := []string{tempFile.Name()}
	folderStorageVideo := fmt.Sprintf("%s/%s/%s", "creator", accaunt, folder)
	fileVideo, err := fb.UploadFilesToFireStorage(context.Background(), videos, folderStorageVideo, hash)
	if err != nil {
		fmt.Println("Ошибка при загрузке в Firestorage:", err)
	}
	fmt.Println(fileVideo)

	ratio, err := ffmpeg.GetAspectRatio(tempFile.Name())
	if err != nil {
		fmt.Println("Ошибка AspectRatio:", err)
	}

	// Создание метаданных
	metadata := fb.VideoCreatorMetadata{
		Accaunt: accaunt,
		Name:    hash,
		Extname: filepath.Ext(tempFile.Name()),
		Folder:  folder,
		Thumbs:  filesPath,
		Created: time.Now(),
		Updated: time.Now(),
		Ratio:   ratio,
	}
	// Запись метаданных в Firestore
	ref, err := fb.SaveVideoCreatorMetadata(context.Background(), metadata, "creator")
	if err != nil {
		fmt.Printf("Ошибка записи метаданных в Firestore: %v", err)
	}

	type VideoCreatorResult struct {
		Success bool                    `json:"success"`
		Id      string                  `json:"id"`
		Data    fb.VideoCreatorMetadata `json:"data"`
	}

	result := VideoCreatorResult{
		Success: true,
		Id:      ref.ID,
		Data:    metadata,
	}

	bytes, err := json.Marshal(result)
	if err != nil {
		log.Printf("Error sending progress message: %v", err)
	}
	conn.WriteMessage(websocket.TextMessage, bytes)
}

func main() {
	// r := mux.NewRouter()
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	segmentDir := "stream"
	http.Handle("/stream/", http.StripPrefix("/stream/", http.FileServer(http.Dir(segmentDir))))

	// r := mux.NewRouter()
	// Загрузка и конвертация видео
	// http.HandleFunc("/convert", convertVideoHandler)
	// r.Handle("/convert", VerifyIDToken(http.HandlerFunc(convertVideoHandler)))
	http.HandleFunc("/convert", convertVideoHandler)
	http.HandleFunc("/poster", creatPosterHandle)
	http.HandleFunc("/vttfile", createVTTHandle)
	http.HandleFunc("/ws-transcription", transcriptionHandlerWS)
	http.HandleFunc("/transcription", transcriptionHandler)
	http.HandleFunc("/upload-video", preprocessVideoHandler)
	// Создание сегментов
	http.HandleFunc("/creatSegments", creatVideoSegmentsHandle)
	// r.Handle("/creatSegments", VerifyIDToken(http.HandlerFunc(creatVideoSegmentsHandle)))
	// headersOk := handlers.AllowedHeaders([]string{"X-Requested-With", "Content-Type", "Authorization"})
	// originsOk := handlers.AllowedOrigins([]string{"http://127.0.0.1:3000"})
	// methodsOk := handlers.AllowedMethods([]string{"GET", "HEAD", "POST", "PUT", "OPTIONS"})

	// fs := http.FileServer(http.Dir("./static"))
	// http.Handle("/", fs)
	port := os.Getenv("HOST") + ":4003"

	// host := "192.168.1.149"
	log.Printf("Сервер запущен на http://%v", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

}
