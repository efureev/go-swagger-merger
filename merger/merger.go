package merger

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Merger struct {
	Swagger map[string]interface{}
	saver   saver
}

func NewMerger() *Merger {
	merger := new(Merger)
	merger.Swagger = map[string]interface{}{}
	return merger
}

func (m *Merger) AddFile(file string) error {
	//f, err := os.Open(file)
	//if err != nil {
	//	return err
	//}
	//defer f.Close()

	content, err := os.ReadFile(file)
	//content, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}

	var s1 interface{}
	err = yaml.Unmarshal(content, &s1)
	if err != nil {
		return err
	}

	return m.merge(s1.(map[string]interface{}))
}

func (m *Merger) merge(f map[string]interface{}) error {
	for key, item := range f {
		switch key {
		case `servers`:
			m.mergeServers(item.([]interface{}))
			continue
		case `tags`:
			m.mergeTags(item.([]interface{}))
			continue
		case `components`:
			m.mergeComponents(item.(map[string]interface{}))
			continue
			//case `paths`:
			//	m.mergePaths(item.(map[string]interface{}))
			//	continue
		}

		if i, ok := item.(map[string]interface{}); ok {
			for subKey, subitem := range i {
				if _, ok := m.Swagger[key]; !ok {
					m.Swagger[key] = map[string]interface{}{}
				}

				m.Swagger[key].(map[string]interface{})[subKey] = subitem
			}
		} else if i, ok := item.([]interface{}); ok {
			if _, ok := m.Swagger[key]; !ok {
				m.Swagger[key] = []interface{}{}
			}
			m.Swagger[key] = append(m.Swagger[key].([]interface{}), i...)
		} else {
			m.Swagger[key] = item
		}
	}

	return nil
}

func (m *Merger) Save(fileName string) error {
	return m.saver.Save(fileName, m.Swagger)
}

func (m *Merger) mergeServers(item []interface{}) {
	origin, ok := m.Swagger[`servers`].([]interface{})
	if !ok {
		origin = []interface{}{}
	}
	m.Swagger[`servers`] = mergeServers(origin, item)
}

func (m *Merger) mergeTags(item []interface{}) {
	origin, ok := m.Swagger[`tags`].([]interface{})
	if !ok {
		origin = []interface{}{}
	}
	m.Swagger[`tags`] = mergeTags(origin, item)
}

func (m *Merger) mergeComponents(item map[string]interface{}) {
	origin, ok := m.Swagger[`components`].(map[string]interface{})
	if !ok {
		origin = map[string]interface{}{}
	}
	m.Swagger[`components`] = mergeComponents(origin, item)
}

//func (m *Merger) mergePaths(item map[string]interface{}) {
//	origin, ok := m.Swagger[`paths`].(map[string]interface{})
//	if !ok {
//		origin = map[string]interface{}{}
//	}
//	m.Swagger[`paths`] = mergePaths(origin, item)
//}
