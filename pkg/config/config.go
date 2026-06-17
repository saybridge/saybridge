package config

import (
	"log"

	"github.com/spf13/viper"
)

// Config contains all application configuration variables loaded from the .env file or system environment variables.
type Config struct {
	Port              string `mapstructure:"PORT"`
	Env               string `mapstructure:"ENV"`
	DBHost            string `mapstructure:"DB_HOST"`
	DBPort            string `mapstructure:"DB_PORT"`
	DBUser            string `mapstructure:"DB_USER"`
	DBPassword        string `mapstructure:"DB_PASSWORD"`
	DBName            string `mapstructure:"DB_NAME"`
	DBSslMode         string `mapstructure:"DB_SSLMODE"`
	RedisHost         string `mapstructure:"REDIS_HOST"`
	RedisPort         string `mapstructure:"REDIS_PORT"`
	RedisPassword     string `mapstructure:"REDIS_PASSWORD"`
	NatsURL           string `mapstructure:"NATS_URL"`
	MeiliURL          string `mapstructure:"MEILI_URL"`
	MeiliKey          string `mapstructure:"MEILI_KEY"`
	MinioEndpoint     string `mapstructure:"MINIO_ENDPOINT"`
	MinioAccessKey    string `mapstructure:"MINIO_ACCESS_KEY"`
	MinioSecretKey    string `mapstructure:"MINIO_SECRET_KEY"`
	MinioUseSSL       bool   `mapstructure:"MINIO_USE_SSL"`
	MinioBucket       string `mapstructure:"MINIO_BUCKET"`
	LivekitURL        string `mapstructure:"LIVEKIT_URL"`
	LivekitAPIKey     string `mapstructure:"LIVEKIT_API_KEY"`
	LivekitSecret     string `mapstructure:"LIVEKIT_API_SECRET"`
	JWTPrivateKeyPath string `mapstructure:"JWT_PRIVATE_KEY_PATH"`
	JWTPublicKeyPath  string `mapstructure:"JWT_PUBLIC_KEY_PATH"`
	EnterpriseEnabled bool   `mapstructure:"ENABLE_ENTERPRISE"`
	CORSOrigins       string `mapstructure:"CORS_ORIGINS"` // Comma-separated allowed origins (e.g. "https://app.example.com,https://admin.example.com"). Empty or "*" = allow all (dev only).
}

// LoadConfig searches for and reads the environment configuration from the specified path.
// If the configuration file is missing, it falls back to the system environment variables.
func LoadConfig(path string) (*Config, error) {
	viper.AddConfigPath(path)
	if path == "." {
		viper.AddConfigPath("backend")
	}
	viper.SetConfigName(".env")
	viper.SetConfigType("env")

	// Overwrite configs from system environment variables if available
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		log.Printf("Warning: Could not find .env configuration file in path %s (or fallback). Using default system environment variables. Error: %v", path, err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}
