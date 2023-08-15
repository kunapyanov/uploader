package main

import (
	"encoding/json"
	"fmt"
	"github.com/jlaffaye/ftp"
	"golang.org/x/exp/slog"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Projects []Project `json:"projects"`
}

type Project struct {
	Name       string    `json:"name"`
	LocalPath  string    `json:"localPath"`
	RemotePath string    `json:"remotePath"`
	Connection FTPConfig `json:"ftpConfig"`
}

type FTPConfig struct {
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
	Port     int    `json:"port"`
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	logger.Debug("Getting current working dir")
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	dir = strings.TrimSuffix(strings.TrimSpace(dir), "/")
	configFilename := dir + "/config.json"
	logger.Debug("Got current dir", slog.String("config path", configFilename))
	if _, err = os.Stat(configFilename); err != nil {
		panic(err)
	}
	configContents, err := os.ReadFile(configFilename)
	if err != nil {
		panic(err)
	}
	var config Config
	if err = json.Unmarshal(configContents, &config); err != nil {
		panic(err)
	}
projectsLoop:
	for _, p := range config.Projects {
		logger = logger.With("project", p.Name)
		logger.Debug("Dial", slog.String("host", p.Connection.Host))
		c, err := ftp.Dial(fmt.Sprintf("%s:%d", p.Connection.Host, p.Connection.Port), ftp.DialWithTimeout(5*time.Second))
		if err != nil {
			logger.Error("dial error", slog.String("error", err.Error()))
			continue
		}
		if err = c.Login(p.Connection.Username, p.Connection.Password); err != nil {
			logger.Error("connection error", slog.String("error", err.Error()))
			continue
		}
		logger.Info("removing everything on remote server")
		var list []*ftp.Entry
		if list, err = c.List(p.RemotePath); err != nil {
			logger.Error("can not get list of remote entries in root", slog.String("error", err.Error()))
			continue
		}
		for _, entry := range list {
			remotePath := p.RemotePath + "/" + entry.Name
			logger.Debug("deleting remote entry", slog.String("remote_path", remotePath))
			if entry.Type == ftp.EntryTypeFile {
				if err = c.Delete(remotePath); err != nil {
					logger.Error("can not delete remote entry", slog.String("remote_path", remotePath), slog.Any("error", err))
					continue projectsLoop
				}
			} else {
				if err = c.RemoveDirRecur(remotePath); err != nil {
					logger.Error("can not delete remote entry", slog.String("remote_path", remotePath), slog.Any("error", err))
					continue projectsLoop
				}
			}
		}
		logger.Info("iterating over local directories", slog.String("path", p.LocalPath))
		if err = filepath.Walk(p.LocalPath, func(path string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if path == p.LocalPath {
				return nil
			}
			remotePath := p.RemotePath + strings.TrimPrefix(path, p.LocalPath)
			logger.Debug("processing path", slog.String("local", path), slog.String("remote", remotePath))
			if info.IsDir() {
				if err = c.MakeDir(remotePath); err != nil {
					logger.Error("can not create remote dir", slog.String("error", err.Error()))
					return err
				}
			} else {
				file, err := os.Open(path)
				if err != nil {
					logger.Error("can not read local file", slog.String("path", path), slog.String("error", err.Error()))
					return err
				}
				if err = c.Stor(remotePath, file); err != nil {
					logger.Error("can not store remote file", slog.String("remote_path", remotePath), slog.String("error", err.Error()))
					return err
				}
			}
			return nil
		}); err != nil {
			logger.Error("error walking", slog.String("path", p.LocalPath), slog.String("error", err.Error()))
		}
		if err = c.Quit(); err != nil {
			logger.Error("error walking", slog.String("error", err.Error()))
		}
	}
}
