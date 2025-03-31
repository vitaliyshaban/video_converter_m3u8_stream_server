package fb

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/storage"
	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
	method "m3u8.com/src/lib/methods"
)

var (
	project string = "alesiafitness-firebase"
	bucket  string = project + ".appspot.com"
)

func FirebaseAuth(ctx context.Context) (*auth.Client, error) {
	config := &firebase.Config{
		ProjectID: project,
	}
	app, err := firebase.NewApp(ctx, config, option.WithoutAuthentication())
	if err != nil {
		log.Fatalf("error getting NewApp: %v\n", err)
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return client, err
	}
	return client, nil
}

func IsAuthAdmin(ctx context.Context, idToken string) (map[string]interface{}, error) {
	client, err := FirebaseAuth(ctx)
	if err != nil {
		return nil, err
	}

	token, err := client.VerifyIDToken(ctx, idToken)
	if err != nil {
		return nil, err
	}
	claims := token.Claims
	if role, ok := claims["role"]; ok {
		if role == "admin" {
			return claims, nil
		}
	}
	return nil, err
}

// Создание клиента Google Cloud Storage
func InitClientStorage(ctx context.Context) (*storage.Client, error) {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	host := fmt.Sprintf("http://%v:%v", os.Getenv("HOST"), 9199)

	client, err := storage.NewClient(ctx, option.WithoutAuthentication(), option.WithEndpoint(host))
	if err != nil {
		return client, err
	}
	defer client.Close()
	return client, nil
}
func InitClientStore(ctx context.Context) (*firestore.Client, error) {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}
	host := fmt.Sprintf("http://%v:%v", os.Getenv("HOST"), 8080)

	// fmt.Println(host)
	// sa := option.WithCredentialsFile("./serviceAccountKey.json")
	client, err := firestore.NewClient(ctx, project, option.WithEndpoint(host))
	if err != nil {
		return nil, err
	}
	// defer client.Close()
	return client, nil
}

// Выгрузка видео из FireStorag
func DownloadVideo(ctx context.Context, video string) error {
	client, err := InitClientStorage(ctx)
	if err != nil {
		return err
	}

	rc, err := client.Bucket(bucket).Object(video).NewReader(ctx)
	if err != nil {
		return err
	}
	defer rc.Close()

	// Создание локального файла для хранения загруженного видео
	localFile, err := os.Create(video)
	if err != nil {
		return err
	}
	defer localFile.Close()

	// Копирование данных из Google Cloud Storage в локальный файл
	if _, err := io.Copy(localFile, rc); err != nil {
		return err
	}
	// Закрытие файла
	localFile.Close()

	return nil
}

func UploadFilesToFireStorage(ctx context.Context, files []string, folderTo string, values ...string) (filesPath []string, err error) {
	if len(files) == 0 {
		log.Fatal("No files found in the directory.")
	}

	filesPath = []string{}

	for _, file := range files {
		fileName := filepath.Base(file)

		if len(values) != 0 {
			fileName = fmt.Sprintf("%s%s", values[0], filepath.Ext(file))
		}
		objectName := filepath.Join(folderTo, fileName)
		filesPath = append(filesPath, fileName)
		f, err := os.Open(file)
		if err != nil {
			return nil, err
		}
		defer f.Close()

		client, err := InitClientStorage(ctx)
		if err != nil {
			return nil, err
		}

		fmt.Println(objectName)
		wc := client.Bucket(bucket).Object(objectName).NewWriter(ctx)
		// Копирование содержимого файла в объект
		if _, err = io.Copy(wc, f); err != nil {
			return nil, err
		}

		// Закрытие writer
		if err := wc.Close(); err != nil {
			return nil, err
		}
		// Удаляем с локального диска
		if err := method.RemoveLocalFile(file); err != nil {
			return nil, err
		}
	}
	return
}

func FileExistsInFirestorage(objectName string) (bool, error) {
	ctx := context.Background()

	client, err := InitClientStorage(ctx)
	if err != nil {
		return false, err
	}

	// Получение атрибутов объекта
	_, err = client.Bucket(bucket).Object(objectName).Attrs(ctx)
	if err != nil {
		if err == storage.ErrObjectNotExist {
			return false, nil
		}
		return false, fmt.Errorf("failed to get object attributes: %v", err)
	}

	return true, nil
}

// VideoMetadata содержит метаданные видео
type VideoMetadata struct {
	Title    string    `firestore:"title"`
	Name     string    `firestore:"name"`
	Hash     string    `firestore:"hash"`
	Extname  string    `firestore:"extname"`
	Storage  bool      `firestore:"storage"`
	Segments bool      `firestore:"segments"`
	Poster   string    `firestore:"poster"`
	Url      string    `firestore:"url"`
	Chapters []Chapter `firestore:"chapters"`
}

type Chapter struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Text  string `json:"text"`
}

// SaveVideoMetadata сохраняет метаданные видео в Firestore
func SaveVideoMetadata(ctx context.Context, metadata VideoMetadata, collection string) error {
	client, err := InitClientStore(ctx)
	if err != nil {
		return err
	}
	fmt.Println(metadata)

	_, _, err = client.Collection(collection).Add(ctx, metadata)
	if err != nil {
		log.Printf("An error has occurred: %s", err)
		return err
	}
	fmt.Printf("Записи метаданных в Firestore успешно произведене!")
	return nil
}

type VideoCreatorMetadata struct {
	Name    string    `firestore:"name"`
	Folder  string    `firestore:"folder"`
	Accaunt string    `firestore:"accaunt"`
	Extname string    `firestore:"extname"`
	Thumbs  []string  `firestore:"thumbs"`
	Created time.Time `firestore:"created"`
	Updated time.Time `firestore:"updated"`
	Ratio   float64   `firestore:"ratio"`
}

func SaveVideoCreatorMetadata(ctx context.Context, metadata VideoCreatorMetadata, collection string) (ref *firestore.DocumentRef, err error) {
	client, err := InitClientStore(ctx)
	if err != nil {
		return nil, err
	}
	// fmt.Println(metadata)

	ref, _, err = client.Collection(collection).Add(ctx, metadata)
	if err != nil {
		log.Printf("An error has occurred: %s", err)
		return nil, err
	}

	fmt.Printf("Записи метаданных в Firestore успешно произведене!")
	return
}

func UpdateVideoMetadata(ctx context.Context, metadata map[string]interface{}, id string) error {
	client, err := InitClientStore(ctx)
	if err != nil {
		return err
	}
	// fmt.Println(metadata)
	_, err = client.Collection("videos").Doc(id).Set(ctx, metadata, firestore.MergeAll)
	if err != nil {
		return err
	}

	fmt.Printf("Записи метаданных в Firestore успешно обновлены!")
	return nil
}
