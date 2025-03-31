package ai

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/gorilla/websocket"
	method "m3u8.com/src/lib/methods"
)

// export WSCRIBE_MODELS_DIR=/Users/vitaliyshaban/Home/development/apps/subtitles/gradio/whisper-models
// os.environ['WSCRIBE_MODELS_DIR']='/Users/vitaliyshaban/Home/development/apps/go/m3u8/whisper-models'
// wscribe transcribe audios/output_audio.wav subtitles/output_audio.json -m large-v2
// wscribe transcribe audios/output_audio.m4a subtitles/output_audio_m4a.json --language Belarusian -m large-v2

// wscribe transcribe output_audio.wav transcription.vtt -f vtt -m large-v2
type TranscriptionResWS struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	File    string `json:"file"`
}

func GetSubtitlesJSONws(conn *websocket.Conn, file string) (err error) {
	hash, err := method.GenerateFileHash(file)
	if err != nil {
		log.Println("Ошибка при генерации хеша файла:", err)
		return err
	}
	filename := fmt.Sprintf("%s/%s_%s.%s", "subtitles", hash, "subtitle", "json")
	cmd := exec.Command("wscribe", "transcribe", file, filename, "-m", "tiny")

	stdout, err := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	for {
		tmp := make([]byte, 1024)
		_, err := stdout.Read(tmp)
		proc := strings.Split(string(tmp), "%|")
		if len(proc) > 1 {
			progress := strings.Trim(proc[0], "\r ")
			result := TranscriptionResWS{
				Success: true,
				Message: progress,
				File:    "",
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
		if err != nil {
			break
		}
	}

	result := TranscriptionResWS{
		Success: true,
		Message: "100",
		File:    filename,
	}

	bytes, err := json.Marshal(result)
	if err != nil {
		log.Printf("Error sending progress message: %v", err)
	}

	err = conn.WriteMessage(websocket.TextMessage, bytes)
	if err != nil {
		log.Printf("Error sending progress message: %v", err)
	}

	return nil
}

func GetSubtitlesJSON() error {
	cmd := exec.Command("wscribe", "transcribe", "audios/output_audio.m4a", "subtitles/output_audio.json", "-m", "large-v2", "-d")

	stdout, err := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	for {
		tmp := make([]byte, 1024)
		_, err := stdout.Read(tmp)
		proc := strings.Split(string(tmp), "%|")
		if len(proc) > 1 {
			fmt.Println(strings.Trim(proc[0], ""))
		}
		if err != nil {
			break
		}
	}

	return nil
}
