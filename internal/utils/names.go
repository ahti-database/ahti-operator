package utils

import (
	"fmt"

	libsqlv1 "github.com/ahti-database/operator/api/v1"
)

func GetAuthSecretName(database *libsqlv1.Database) string {
	return fmt.Sprintf("%v-auth-key", database.Name)
}
