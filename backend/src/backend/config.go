package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

var defaultFileContent = []byte(`{
    "verbose": true,
    "server" : {
        "address"    : "127.0.0.1:8081",
		"pathprefix" : "/api/",
		"dbmanagment": false,
        "cors"       : true
    },
    "paths" : {
        "databases"    : "~databases",
        "results"      : "~jobs",
        "mmseqs"       : "~mmseqs"
    },
    "redis" : {
        "network"  : "tcp",
        "address"  : "localhost:6379",
        "password" : "",
        "index"    : 0
    },
    "mail" : {
        "type"      : "null",
        "sender"    : "mail@example.org",
        "templates" : {
            "success" : {
                "subject" : "Done -- %s",
                "body"    : "%s"
            },
            "timeout" : {
                "subject" : "Timeout -- %s",
                "body"    : "%s"
            },
            "error"   : {
                "subject" : "Error -- %s",
                "body"    : "%s"
            }
        }
    }
}
`)

type ConfigPaths struct {
	Databases string `json:"databases"`
	Results   string `json:"results"`
	Temporary string `json:"temporary"`
	Mmseqs    string `json:"mmseqs"`
}

type ConfigRedis struct {
	Network  string `json:"network"`
	Address  string `json:"address"`
	Password string `json:"password"`
	DbIndex  int    `json:"index"`
}

type ConfigMailTemplate struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type ConfigMailTemplates struct {
	Success ConfigMailTemplate `json:"success"`
	Timeout ConfigMailTemplate `json:"timeout"`
	Error   ConfigMailTemplate `json:"error"`
}

type ConfigMail struct {
	Transport string              `json:"type"`
	Sender    string              `json:"sender"`
	Templates ConfigMailTemplates `json:"templates"`
}

type ConfigAuth struct {
	Username string `json:"username" valid:"required"`
	Password string `json:"password" valid:"required"`
}

type ConfigServer struct {
	Address     string      `json:"address" valid:"required"`
	PathPrefix  string      `json:"pathprefix" valid:"optional"`
	DbManagment bool        `json:"dbmanagment" valid:"optional"`
	CORS        bool        `json:"cors" valid:"optional"`
	Auth        *ConfigAuth `json:"auth" valid:"optional"`
}

type ConfigRoot struct {
	Server  ConfigServer `json:"server" valid:"required"`
	Paths   ConfigPaths  `json:"paths" valid:"required"`
	Redis   ConfigRedis  `json:"redis" valid:"optional"`
	Mail    ConfigMail   `json:"mail" valid:"optional"`
	Verbose bool         `json:"verbose"`
}

func ReadConfigFromFile(name string) (ConfigRoot, error) {
	file, err := os.Open(name)
	if err != nil {
		return ConfigRoot{}, err
	}
	defer file.Close()

	absPath, err := filepath.Abs(name)
	if err != nil {
		return ConfigRoot{}, err
	}

	relativeTo := filepath.Dir(absPath)

	return ReadConfig(file, relativeTo)
}

func DefaultConfig() (ConfigRoot, error) {
	r := bytes.NewReader(defaultFileContent)

	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	relativeTo := filepath.Dir(ex)

	return ReadConfig(r, relativeTo)
}

func ReadConfig(r io.Reader, relativeTo string) (ConfigRoot, error) {
	var config ConfigRoot
	if err := DecodeJsonAndValidate(r, &config); err != nil {
		return config, fmt.Errorf("Fatal error for config file: %s\n", err)
	}

	paths := []*string{&config.Paths.Databases, &config.Paths.Results, &config.Paths.Mmseqs}
	for _, path := range paths {
		if strings.HasPrefix(*path, "~") {
			*path = strings.TrimLeft(*path, "~")
			*path = filepath.Join(relativeTo, *path)
		}
	}

	return config, nil
}

func (c *ConfigRoot) CheckPaths() bool {
	paths := []string{c.Paths.Databases, c.Paths.Results}
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.MkdirAll(path, 0755)
		}
	}

	if _, err := os.Stat(c.Paths.Mmseqs); err != nil {
		return false
	}

	return true
}

func (c *ConfigRoot) ReadParameters(args []string) error {
	var key string
	inParameter := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			if inParameter == true {
				return errors.New("Invalid Parameter String")
			}
			key = strings.TrimLeft(arg, "-")
			inParameter = true
		} else {
			if inParameter == false {
				return errors.New("Invalid Parameter String")
			}
			err := c.setParameter(key, arg)
			if err != nil {
				return err
			}
			inParameter = false
		}
	}

	if inParameter == true {
		return errors.New("Invalid Parameter String")
	}

	return nil
}

func (c *ConfigRoot) setParameter(key string, value string) error {
	path := strings.Split(key, ".")
	return setNodeValue(c, path, value)
}

// DFS in Config Tree to set the new value
func setNodeValue(node interface{}, path []string, value string) error {
	if len(path) == 0 {
		if v, ok := node.(reflect.Value); ok {
			if v.IsValid() == false || v.CanSet() == false {
				return errors.New("Leaf node is not valid")
			}

			switch v.Kind() {
			case reflect.Struct:
				return errors.New("Leaf node is a struct")
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				i, err := strconv.ParseInt(value, 10, 64)
				if err != nil {
					return err
				}
				v.SetInt(i)
				break
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				i, err := strconv.ParseUint(value, 10, 64)
				if err != nil {
					return err
				}
				v.SetUint(i)
				break
			case reflect.Bool:
				b, err := strconv.ParseBool(value)
				if err != nil {
					return err
				}
				v.SetBool(b)
				break
			case reflect.String:
				v.SetString(value)
				break
			default:
				return errors.New("Leaf node type not implemented")
			}
			return nil
		} else {
			return errors.New("Leaf node is not a value")
		}
	}

	v, ok := node.(reflect.Value)
	if !ok {
		v = reflect.ValueOf(node).Elem()
	}

	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			t := v.Type().Elem()
			n := reflect.New(t)
			v.Set(n)
		}
		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return errors.New("Node is not a struct")
	}

	for i := 0; i < v.NumField(); i++ {
		tag := v.Type().Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}

		if tag == path[0] {
			f := v.Field(i)
			return setNodeValue(f, path[1:], value)
		}
	}

	return errors.New("Path not found in config")
}