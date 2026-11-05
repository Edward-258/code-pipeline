package repo

import (
	"errors"
	"io"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/gridfs"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 产物归档（T2.4）：GridFS bucket "artifacts"。
// 对照 Zadig 的 S3 对象存储——学习版砍 S3，GridFS 就是"Mongo 自带的文件系统"，
// 32MB 上限内零新依赖（引 S3 要 minio client + 凭证体系，学习版不值）。
// 键 = "<workflow>-<taskID>-<step>"：天然按任务归档，同名重传覆盖（UploadFromStream
// 同名 = 新文件版本，DownloadByName 取最新——GridFS 语义正好是我们要的幂等重跑）。

var artifactBucket *gridfs.Bucket

func initArtifactBucket() (err error) {
	artifactBucket, err = gridfs.NewBucket(client.Database("pipeline"), options.GridFSBucket().SetName("artifacts"))
	return err
}

// PutArtifact —— 上传产物（上限约束由 API 层用 MaxBytesReader 执行，这里只管落库）
func PutArtifact(name string, r io.Reader) error {
	if artifactBucket == nil {
		if err := initArtifactBucket(); err != nil {
			return err
		}
	}
	_, err := artifactBucket.UploadFromStream(name, r)
	return err
}

var ErrArtifactNotFound = errors.New("artifact not found")

// GetArtifact —— 下载产物流（调用方负责关 reader）
func GetArtifact(name string) (io.ReadCloser, error) {
	if artifactBucket == nil {
		if err := initArtifactBucket(); err != nil {
			return nil, err
		}
	}
	stream, err := artifactBucket.OpenDownloadStreamByName(name)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) || errors.Is(err, gridfs.ErrFileNotFound) {
			return nil, ErrArtifactNotFound
		}
		return nil, err
	}
	return stream, nil
}
