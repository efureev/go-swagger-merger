package merger

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type saver struct {
}

func (s *saver) Save(fileName string, data map[string]interface{}) error {
	res, err := marshal(data)
	if err != nil {
		return err
	}

	f, err := os.Create(fileName)
	if err != nil {
		return err
	}

	defer f.Close()

	_, err = f.Write(res)
	if err != nil {
		return err
	}

	return nil
}

func marshal(o interface{}) ([]byte, error) {

	j, err := json.Marshal(o)
	if err != nil {
		return nil, fmt.Errorf("error marshaling into JSON: %v", err)
	}

	y, err := jsonToYaml(j)
	if err != nil {
		return nil, fmt.Errorf("error converting JSON to YAML: %v", err)
	}

	return y, nil
}

func jsonToYaml(j []byte) ([]byte, error) {
	var jsonObj interface{}
	err := yaml.Unmarshal(j, &jsonObj)
	if err != nil {
		return nil, err
	}

	return yaml.Marshal(jsonObj)
}
