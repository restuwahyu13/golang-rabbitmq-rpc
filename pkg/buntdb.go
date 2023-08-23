package pkg

import (
	"os"
	"reflect"
	"runtime"
	"time"

	"github.com/bytedance/sonic"
	"github.com/sirupsen/logrus"
	"github.com/tidwall/buntdb"
)

type BuntDatabaseInterface interface {
	Set(key string, value interface{}) error
	SetEx(key string, value interface{}, ttl time.Duration) error
	Get(key string) ([]byte, error)
	Del(key string) error
}

type buntDatabaseStruct struct {
	db *buntdb.DB
}

func NewBuntDB() BuntDatabaseInterface {
	dbName := ".buntdb.db"
	dbLocation := os.TempDir() + "/" + dbName

	if val, ok := os.LookupEnv("GO_ENV"); ok && val != "production" && runtime.GOOS != "windows" {
		dir, _ := os.UserHomeDir()
		dbLocation = dir + "/" + dbName

	} else if val, ok := os.LookupEnv("GO_ENV"); ok && val != "development" && runtime.GOOS != "windows" {
		dir, _ := os.UserHomeDir()
		dbLocation = dir + "/" + dbName
	}

	db, err := buntdb.Open(dbLocation)
	if err != nil {
		logrus.Fatalf("NewBuntDB - buntdb.Open Error: %s", err.Error())
	}

	return &buntDatabaseStruct{db: db}
}

func (h *buntDatabaseStruct) Set(key string, value interface{}) error {
	if err := h.db.CreateIndex(key, "*", buntdb.IndexString); err != nil {
		return err
	}

	return h.db.Update(func(tx *buntdb.Tx) error {
		var resByte []byte

		if !reflect.ValueOf(value).CanConvert(reflect.TypeOf([]byte{})) {
			valueByte, err := sonic.Marshal(value)

			if err != nil {
				return err
			}

			resByte = valueByte
		} else {
			resByte = value.([]byte)
		}

		if _, _, err := tx.Set(key, string(resByte), nil); err != nil {
			return err
		}

		return nil
	})
}

func (h *buntDatabaseStruct) SetEx(key string, value interface{}, ttl time.Duration) error {
	if err := h.db.CreateIndex(key, "*", buntdb.IndexString); err != nil {
		return err
	}

	return h.db.Update(func(tx *buntdb.Tx) error {
		var resByte []byte

		if !reflect.ValueOf(value).CanConvert(reflect.TypeOf([]byte{})) {
			valueByte, err := sonic.Marshal(value)

			if err != nil {
				return err
			}

			resByte = valueByte
		} else {
			resByte = value.([]byte)
		}

		if _, _, err := tx.Set(key, string(resByte), &buntdb.SetOptions{Expires: true, TTL: time.Duration(time.Second * ttl)}); err != nil {
			return err
		}

		return nil
	})
}

func (h *buntDatabaseStruct) Get(key string) ([]byte, error) {
	var res []byte

	err := h.db.View(func(tx *buntdb.Tx) error {
		err := tx.Descend(key, func(k, _ string) bool {

			cache, err := tx.Get(k)

			if err != nil {
				return false
			}

			res = []byte(cache)
			return true
		})

		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	defer h.db.DropIndex(key)
	return res, nil
}

func (h *buntDatabaseStruct) Del(key string) error {
	err := h.db.Update(func(tx *buntdb.Tx) error {
		_, err := tx.Delete(key)

		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}
