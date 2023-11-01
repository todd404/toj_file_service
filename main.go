package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/google/uuid"

	_ "image/gif"

	_ "image/jpeg"
)

// 定义一些常量
const (
	configFile  = "config.json"
	uploadDir   = "files/upload" // 上传文件的目录
	avatarDir   = "files/avatar" // 头像文件的目录
	answerDir   = "files/answer" // 答案文件的目录
	testDir     = "files/test"   // 测试文件的目录
	maxFileSize = 10 << 20       // 上传文件的最大大小（10MB）
	pngExt      = ".png"         // png文件的扩展名
	txtExt      = ".txt"         // txt文件的扩展名
)

// 定义一个结构体，用于返回上传文件的uuid
type uploadResponse struct {
	FileUUID string `json:"file_uuid"`
}

// 定义一个结构体，用于接收设置文件名的请求参数
type setRequest struct {
	FileUUID string `json:"file_uuid"`
	FileName string `json:"file_name"`
}

type setResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// 定义一个统一的上传接口，将上传的文件用唯一的uuid重命名，然后返回{file_uuid: uuid}
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法是否为POST
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 解析表单数据
	err := r.ParseMultipartForm(maxFileSize)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 获取上传的文件
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()
	// 获取上传文件的原始扩展名
	ext := filepath.Ext(header.Filename)
	// 生成一个唯一的uuid作为新的文件名
	uuid := uuid.New().String()
	newName := uuid + ext
	// 创建上传目录（如果不存在）
	err = os.MkdirAll(uploadDir, os.ModePerm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// 在上传目录中创建新的文件
	newFile, err := os.Create(filepath.Join(uploadDir, newName))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer newFile.Close()
	// 将上传的文件内容复制到新的文件中
	io.Copy(newFile, file)
	// 返回{file_uuid: uuid}给客户端
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(uploadResponse{FileUUID: uuid})
}

var SetFileResponseFunc = func(w *http.ResponseWriter, success bool, msg string) {
	var response setResponse

	(*w).Header().Set("Content-type", "application/json")
	response.Success = success
	response.Message = msg
	json.NewEncoder((*w)).Encode(&response)
}

// 定义一个函数，用于将file_uuid指定的文件尝试转换为png,然后放在对应的avatar文件夹
func setAvatarHandler(w http.ResponseWriter, r *http.Request) {
	// 检查请求方法是否为POST
	if r.Method != http.MethodPost {
		SetFileResponseFunc(&w, false, "Not post method")
		return
	}
	// 解析JSON数据
	var req setRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		SetFileResponseFunc(&w, false, "Bad json")
		return
	}
	defer r.Body.Close()
	// 获取请求参数中的file_uuid和file_name
	fileUUID := req.FileUUID
	fileName := req.FileName + pngExt // 添加png扩展名
	if fileUUID == "" || fileName == "" {
		SetFileResponseFunc(&w, false, "Missing param")
		return
	}
	// 在上传目录中查找file_uuid对应的文件
	files, err := filepath.Glob(filepath.Join(uploadDir, fileUUID+"*"))
	if err != nil {
		SetFileResponseFunc(&w, false, err.Error())
		return
	}
	if len(files) == 0 {
		SetFileResponseFunc(&w, false, "File not found")
		return
	}
	oldFile := files[0]
	// 创建头像目录（如果不存在）
	err = os.MkdirAll(avatarDir, os.ModePerm)
	if err != nil {
		SetFileResponseFunc(&w, false, err.Error())
		return
	}
	// 在头像目录中创建新的文件
	newFile := filepath.Join(avatarDir, fileName)
	// 尝试将旧文件转换为png格式，并保存到新文件中
	err = convertToPNG(oldFile, newFile)
	if err != nil {
		SetFileResponseFunc(&w, false, err.Error())
		return
	}

	err = os.Remove(oldFile)
	if err != nil {
		SetFileResponseFunc(&w, false, err.Error())
		return
	}

	// 返回成功的消息给客户端
	SetFileResponseFunc(&w, true, "")
}

func MoveFile(sourcePath, destPath string) error {
	inputFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("couldn't open source file: %s", err)
	}
	outputFile, err := os.Create(destPath)
	if err != nil {
		inputFile.Close()
		return fmt.Errorf("couldn't open dest file: %s", err)
	}
	defer outputFile.Close()
	_, err = io.Copy(outputFile, inputFile)
	inputFile.Close()
	if err != nil {
		return fmt.Errorf("writing to output file failed: %s", err)
	}
	// The copy was successful, so now delete the original file
	err = os.Remove(sourcePath)
	if err != nil {
		return fmt.Errorf("fail removing original file: %s", err)
	}
	return nil
}

// 定义一个函数，用于将file_uuid指定的文件改成txt后缀放在answer或test文件夹中
func setFileHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 检查请求方法是否为POST
		if r.Method != http.MethodPost {
			SetFileResponseFunc(&w, false, "Method not allowed")
			return
		}
		// 解析JSON数据
		var req setRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			SetFileResponseFunc(&w, false, err.Error())
			return
		}
		defer r.Body.Close()
		// 获取请求参数中的file_uuid和file_name
		fileUUID := req.FileUUID
		fileName := req.FileName + txtExt // 添加txt扩展名
		if fileUUID == "" || fileName == "" {
			SetFileResponseFunc(&w, false, "Missing parameters")
			return
		}
		// 在上传目录中查找file_uuid对应的文件
		files, err := filepath.Glob(filepath.Join(uploadDir, fileUUID+"*"))
		if err != nil {
			SetFileResponseFunc(&w, false, err.Error())
			return
		}
		if len(files) == 0 {
			SetFileResponseFunc(&w, false, "File not found")
			return
		}
		oldFile := files[0]
		newFile := filepath.Join(dir, fileName) // 新文件的路径

		err = os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			SetFileResponseFunc(&w, false, err.Error())
			return
		}

		err = MoveFile(oldFile, newFile)
		if err != nil {
			SetFileResponseFunc(&w, false, err.Error())
			return
		}

		// 返回成功的消息给客户端
		SetFileResponseFunc(&w, true, "")
	}
}

// 定义一个函数，用于将任意格式的图片文件转换为png格式（需要使用image包和image/png包）
func convertToPNG(src string, dst string) error {
	// 打开源文件，获取图片数据
	file, err := os.Open(src)
	if err != nil {
		return err
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return err
	}
	// 创建目标文件，写入png数据
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	return png.Encode(out, img)
}
func downloadFileHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 获取请求中的文件名或文件UUID
		filename := path.Base(r.URL.Path)
		filePath := fmt.Sprintf("%s/%s", dir, filename)

		// 检查文件是否存在
		_, err := os.Stat(filePath)
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}

		// 打开文件
		file, err := os.Open(filePath)
		if err != nil {
			http.Error(w, "Failed to open file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// 设置响应头，告诉浏览器文件的内容类型
		contentType := mime.TypeByExtension(path.Ext(filename))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)

		// 将文件内容复制到响应中
		_, err = io.Copy(w, file)
		if err != nil {
			http.Error(w, "Failed to copy file to response", http.StatusInternalServerError)
			return
		}
	}
}

type Config struct {
	Port int `json:"port"`
}

func readConfig() (Config, error) {
	data, err := os.ReadFile(configFile)

	if err != nil {
		return Config{}, err
	}

	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		return Config{}, err
	}

	return config, nil
}

func main() {
	config, err := readConfig()
	if err != nil {
		log.Fatalf("Load config.json error: %v", err.Error())
		os.Exit(1)
	}

	// 注册路由处理器
	http.HandleFunc("/upload", uploadHandler)                 // 上传接口
	http.HandleFunc("/set_avatar", setAvatarHandler)          // 设置头像接口
	http.HandleFunc("/set_answer", setFileHandler(answerDir)) // 设置答案接口
	http.HandleFunc("/set_test", setFileHandler(testDir))     // 设置测试接口
	http.HandleFunc("/avatar/", downloadFileHandler(avatarDir))
	http.HandleFunc("/test/", downloadFileHandler(testDir))
	http.HandleFunc("/answer/", downloadFileHandler(answerDir))

	log.Printf("Server started on port: %d\n", config.Port)
	http.ListenAndServe(fmt.Sprintf(":%d", config.Port), nil)
}
