package ffmpeg

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
	method "m3u8.com/src/lib/methods"
)

var (
	hlsTime string = "30" // длинна сегмента видео в секундах
)

type ProgressData struct {
	Success          bool     `json:"success"`
	Error            string   `json:"error"`
	Resolutions      int      `json:"resolutions"`
	TotalResolutions int      `json:"totalResolutions"`
	Progress         float64  `json:"progress"`
	Size             []string `json:"size"`
}

func CreatFramesVideo(inputFilePath string, conn *websocket.Conn, folder string) error {
	frameCount, err := getFrameCount(inputFilePath)
	if err != nil {
		log.Fatalf("Error getting frame count: %v", err)
	}

	duration, err := getVideoDurationInSeconds(inputFilePath)
	if err != nil {
		log.Println("Ошибка при получении продолжительности видео:", err)
		return err
	}

	log.Println(frameCount, duration)
	// cmd := exec.Command("ffmpeg", "-i", inputFilePath, "-ss", "00:00:00", "-q:v", "15", "-vf", "fps=1:0,scale=160:-1", fmt.Sprintf("%s/frame_%s.jpg", folder, "%03d"))
	// cmd := exec.Command("ffmpeg", "-i", inputFilePath, "-ss", "00:00:04", "-frames:v", "1", fmt.Sprintf("%s/frame_%s.jpg", folder, "%03d"))
	cmd := exec.Command("ffmpeg", "-i", inputFilePath, "-vf", "select='not(mod(t,1))',scale=160:-1", "-vsync", "vfr", "-q:v", "2", fmt.Sprintf("%s/frame_%s.jpg", folder, "%03d"))

	// ffmpeg -i input.mp4 -vf "select='not(mod(t,1))',scale=320:240" -vsync vfr -q:v 2 output_%04d.jpg

	var stderr strings.Builder
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("ffmpeg command failed: %v, %v", err, stderr.String())
	}

	return nil
}
func getFrameCount(videoPath string) (int, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=nb_frames", "-of", "default=nokey=1:noprint_wrappers=1", videoPath)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe command failed: %v", err)
	}

	frameCountStr := strings.TrimSpace(string(output))
	frameCount, err := strconv.Atoi(frameCountStr)
	if err != nil {
		return 0, fmt.Errorf("failed to convert frame count to integer: %v", err)
	}

	return frameCount, nil
}

func GetAspectRatio(videoFile string) (aspectRatio float64, err error) {
	// Команда для вызова ffprobe и получения ширины и высоты видео
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "csv=s=x:p=0", videoFile)
	output, err := cmd.Output()
	if err != nil {
		fmt.Println("Ошибка выполнения команды ffprobe:", err)
		return 0, err
	}

	// Парсинг результата
	result := strings.TrimSpace(string(output))
	dimensions := strings.Split(result, "x")
	if len(dimensions) != 2 {
		fmt.Println("Не удалось получить ширину и высоту видео")
		return 0, err
	}

	width, err := strconv.Atoi(dimensions[0])
	if err != nil {
		fmt.Println("Ошибка парсинга ширины:", err)
		return 0, err
	}

	height, err := strconv.Atoi(dimensions[1])
	if err != nil {
		fmt.Println("Ошибка парсинга высоты:", err)
		return 0, err
	}

	// Вычисление соотношения сторон
	aspectRatio = float64(width) / float64(height)

	return

}

func ConvertVideo(inputFilePath string, resolutions []string, conn *websocket.Conn) error {
	duration, err := getVideoDurationInSeconds(inputFilePath)
	if err != nil {
		log.Println("Ошибка при получении продолжительности видео:", err)
		return err
	}

	hash, err := method.GenerateFileHash(inputFilePath)
	if err != nil {
		log.Println("Ошибка при генерации хеша файла:", err)
		return err
	}

	for n, resolution := range resolutions {
		parts := strings.Split(resolution, "x")
		if len(parts) != 2 {
			return fmt.Errorf("неверный формат разрешения: %s", resolution)
		}

		width := parts[0]
		height := parts[1]
		outputDirName := "output"

		outputFileName := fmt.Sprintf("%s/%s_%s_%s.mp4", outputDirName, hash, width, height)

		cmd := exec.Command("ffmpeg", "-i", inputFilePath, "-vf", fmt.Sprintf("scale=%s:%s", width, height), "-c:a", "copy", outputFileName, "-progress", "-")

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		// stderr, err := cmd.StderrPipe()
		// if err != nil {
		// 	return err
		// }

		err = cmd.Start()
		if err != nil {
			return err
		}

		scanner := bufio.NewScanner(stdout)

		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "out_time=") {
				timeStr := strings.Split(line, "=")[1]
				currentTimeInSeconds, err := parseDuration(timeStr)
				if err == nil {
					progress := (currentTimeInSeconds / duration) * 100

					result := ProgressData{
						Success:          true,
						Error:            "",
						Resolutions:      len(resolutions),
						TotalResolutions: n,
						Progress:         progress,
						Size:             parts,
					}

					fmt.Println(result)
					bytes, err := json.Marshal(result)
					if err != nil {
						log.Printf("Error sending progress message: %v", err)
						break
					}

					err = conn.WriteMessage(websocket.TextMessage, bytes)
					if err != nil {
						log.Printf("Error sending progress message: %v", err)
						break
					}
				}
			}
		}

		err = cmd.Wait()
		if err != nil {
			return err
		}
		fmt.Printf("Видео конвертировано в разрешение %s\n", resolution)
	}

	return nil
}

// createMasterM3U8 создает мастер-файл манифеста M3U8 для различных разрешений
func CreateMasterM3U8(filename string, segments map[string]string) error {
	// Открываем файл для записи
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Записываем заголовок файла манифеста
	_, err = fmt.Fprintln(file, "#EXTM3U")
	if err != nil {
		return err
	}

	// Добавляем информацию о каждом разрешении
	for resolution, segmentFile := range segments {
		// Записываем информацию о начале потока для каждого разрешения
		_, err = fmt.Fprintf(file, "#EXT-X-STREAM-INF:BANDWIDTH=2000000,RESOLUTION=%s\n", resolution)
		if err != nil {
			return err
		}
		// Записываем ссылку на сегментный файл m3u8
		_, err = fmt.Fprintf(file, "%s\n", segmentFile)
		if err != nil {
			return err
		}
	}
	fmt.Printf("Файл манифеста создан успешно: %v\n", filename)
	if err := EditFile(filename); err != nil {
		fmt.Printf("Ошибка редактирования файла маниыеста %v: %v\n", filename, err)
	}

	return nil
}

// createSegments создает сегменты видео из исходного файла с указанным разрешением
func CreateSegments(inputFile, outputPrefix, resolution string) error {
	cmd := exec.Command("ffmpeg", "-i", inputFile, "-c:v", "libx264", "-profile:v", "baseline", "-level", "3.0", "-s", resolution, "-start_number", "0", "-hls_time", hlsTime, "-hls_list_size", "0", "-f", "hls", "-strftime_mkdir", "1", "-hls_segment_filename", outputPrefix+"%v_%03d.ts", outputPrefix+".m3u8")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return err
	}
	filePath := outputPrefix + ".m3u8"
	fmt.Printf("Файл сегмента создан успешно: %v\n", filePath)
	if err := EditFile(filePath); err != nil {
		fmt.Printf("Ошибка редактирования файла сегмента %v: %v\n", filePath, err)
	}

	return nil
}

func getVideoDurationInSeconds(filename string) (float64, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", filename)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("ошибка при выполнении ffprobe: %v", err)
	}

	durationStr := strings.TrimSpace(string(output))
	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("ошибка при преобразовании продолжительности: %v", err)
	}

	return duration, nil
}

func parseDuration(durationStr string) (float64, error) {
	parts := strings.Split(durationStr, ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid time format: %s", durationStr)
	}

	hours, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid hours: %s", parts[0])
	}

	minutes, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid minutes: %s", parts[1])
	}

	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid seconds: %s", parts[2])
	}

	totalSeconds := hours*3600 + minutes*60 + seconds
	return totalSeconds, nil
}

func EditFile(filePath string) error {
	// Открытие файла для чтения
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Создание временного файла для записи обновленного содержимого
	tempFilePath := "temp.txt"
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		return err
	}
	defer tempFile.Close()

	// Чтение файла построчно и замена строк
	scanner := bufio.NewScanner(file)
	writer := bufio.NewWriter(tempFile)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "segments/") {
			line = strings.ReplaceAll(line, "/", "%2F")
		}

		line = strings.Replace(line, ".ts", ".ts?alt=media", -1)

		if _, err := writer.WriteString(line + "\n"); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Сбрасывание буфера записи
	if err := writer.Flush(); err != nil {
		return err
	}

	// Замена оригинального файла обновленным содержимым из временного файла
	if err := os.Rename(tempFilePath, filePath); err != nil {
		return err
	}

	return nil
}

func CreatePoster(videoPath, outputDir string, timestamp string, hash string) (string, error) {
	// Проверка, существует ли выходной каталог
	if _, err := os.Stat(outputDir); os.IsNotExist(err) {
		err = os.MkdirAll(outputDir, os.ModePerm)
		if err != nil {
			return "", fmt.Errorf("failed to create output directory: %v", err)
		}
	}

	// Определение выходного пути для изображения
	// if err := method.RemoveLocalFile("posters/poster.png"); err != nil {
	// 	return "", err
	// }
	outputPath := filepath.Join(outputDir, hash+".jpg")

	// Команда FFmpeg для извлечения кадра
	cmd := exec.Command("ffmpeg", "-i", videoPath, "-ss", timestamp, "-vframes", "1", outputPath)

	// Запуск команды
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to create poster: %v", err)
	}

	return outputPath, nil
}
