package config

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"jeda/pkg/strutil"
)

type Config struct {
	Port       string
	RedisURL   string
	APIKey     string
	SigningKey string
}

func LoadConfig() *Config {
	_ = godotenv.Load() // ignore error, it's fine if .env doesn't exist yet

	port := getEnv("PORT", "3001")
	redisURL := getEnv("REDIS_URL", "localhost:6379")

	apiKey := os.Getenv("JEDA_API_KEY")
	signingKey := os.Getenv("JEDA_SIGNING_KEY")
	needsSave := false

	if apiKey == "" {
		apiKey = strutil.GenerateRandomKey("jd_api")
		needsSave = true
		log.Println("WARN: Kunci JEDA_API_KEY tidak ditemukan. Membangkitkan kunci baru...")
	}

	if signingKey == "" {
		signingKey = strutil.GenerateRandomKey("jd_sig")
		needsSave = true
		log.Println("WARN: Kunci JEDA_SIGNING_KEY tidak ditemukan. Membangkitkan kunci baru...")
	}

	if needsSave {
		saveToEnv(apiKey, signingKey)
	}

	// Always print welcome screen
	printWelcomeScreen(apiKey, signingKey)

	return &Config{
		Port:       port,
		RedisURL:   redisURL,
		APIKey:     apiKey,
		SigningKey: signingKey,
	}
}

func getEnv(key, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func saveToEnv(apiKey, signingKey string) {
	envMap, err := godotenv.Read()
	if err != nil {
		envMap = make(map[string]string)
	}
	envMap["JEDA_API_KEY"] = apiKey
	envMap["JEDA_SIGNING_KEY"] = signingKey

	if err := godotenv.Write(envMap, ".env"); err != nil {
		log.Printf("ERROR: Gagal menyimpan kredensial ke .env: %v\n", err)
	}
}

func printWelcomeScreen(apiKey, signingKey string) {
	fmt.Println("\n========================================================================")
	fmt.Println("🚀 JEDA SELF-HOSTED SIAP DIGUNAKAN!")
	fmt.Println("========================================================================")
	fmt.Printf("🔑 API KEY (Kirim Task)    : %s\n", apiKey)
	fmt.Printf("🔑 SIGNING KEY (Verifikasi): %s\n", signingKey)
	fmt.Println()
	fmt.Println("INFO: Kredensial di atas telah otomatis disimpan ke file .env.")
	fmt.Println("Silahkan copy SIGNING KEY ke sisi aplikasi client penerima webhook Anda.")
	fmt.Println("========================================================================")
}
