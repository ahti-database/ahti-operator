package utils

import (
	"fmt"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
)

const (
	EventNormal  string = "Normal"
	EventWarning string = "Warning"
)

func GetAuthSecretName(database *libsqlv1.Database) string {
	return fmt.Sprintf("%v-auth-key", database.Name)
}

func GetDatabasePVCName(database *libsqlv1.Database) string {
	return fmt.Sprintf("%v-pvc", database.Name)
}

func GetDatabaseServiceName(database *libsqlv1.Database, headless bool) string {
	if headless {
		return fmt.Sprintf("%v-svc-headless", database.Name)
	}
	return fmt.Sprintf("%v-svc", database.Name)
}
