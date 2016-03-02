package main
import (
	"os"
	"strconv"
)

var PORT_ENV_PREFIX = "AUTO_PORT"

func GetEnv(name string) string {
	return os.Getenv(name)
}

func SetEnv(name string, value string) {
	os.Setenv(name, value)
}

func ExportPortEnv(ports []int) {
	for i, port := range ports {
		SetEnv(PORT_ENV_PREFIX + strconv.Itoa(i), strconv.Itoa(port))
	}
}
