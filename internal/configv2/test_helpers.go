package configv2

import (
	"os"
	"testing"

	"github.com/census-instrumentation/opencensus-service/internal/configmodels"
	"github.com/spf13/viper"
)

// LoadConfigFile loads a config from file.
func LoadConfigFile(t *testing.T, fileName string) (*configmodels.ConfigV2, error) {
	// Open the file for reading.
	file, err := os.Open(fileName)
	if err != nil {
		t.Error(err)
		return nil, err
	}

	// Read yaml config from file
	v := viper.New()
	v.SetConfigType("yaml")
	err = v.ReadConfig(file)
	if err != nil {
		t.Errorf("unable to read yaml, %v", err)
		return nil, err
	}

	// Load the config from viper
	return Load(v)
}
