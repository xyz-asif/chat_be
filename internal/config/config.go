package config

import (
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	Port              string `mapstructure:"PORT"`
	MongoDBURI        string `mapstructure:"MONGODB_URI"`
	DatabaseName      string `mapstructure:"DATABASE_NAME"`
	FirebaseProjectID string `mapstructure:"FIREBASE_PROJECT_ID"`
	FirebaseCredsPath string `mapstructure:"FIREBASE_CREDS_PATH"` // Path to serviceAccountKey.json
	JWTSecret         string `mapstructure:"JWT_SECRET"`
	JWTExpire         string `mapstructure:"JWT_EXPIRE"`
}

func LoadConfig() (*Config, error) {
	viper.SetConfigFile(".env")
	viper.AutomaticEnv()

	// Set defaults
	viper.SetDefault("PORT", "8080")
	viper.SetDefault("DATABASE_NAME", "anchor_db")

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		// Config file not found; ignore error if desired, or log it
		log.Println("No .env file found, relying on environment variables")
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}

func Load() *Config {
	cfg, err := LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	return cfg
}
