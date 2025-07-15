// Copyright 2019 Ka-Hing Cheung
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"time"

	obs "github.com/huaweicloud/huaweicloud-sdk-go-obs/obs"
	"github.com/jacobsa/fuse"
	. "github.com/kahing/goofys/api/common"
)

type HuaweiObsBackend struct {
	cap Capabilities

	bucket string
	flags  *FlagStorage
	config *HuaweiObsConfig
	client *obs.ObsClient
}

// NewHuaweiObs creates a new Huawei OBS backend with the specified bucket and configuration.
func NewHuaweiObs(bucket string, flags *FlagStorage, config *HuaweiObsConfig) (*HuaweiObsBackend, error) {
	obsClient, err := obs.New(config.AccessKey, config.SecretKey, flags.Endpoint)
	if err != nil {
		log.Errorf("Failed to create OBS client: %v", err)
		obsClient.Close()
	}

	if flags.DebugS3 {
		var logFullPath string = "/dev/null"
		var maxLogSize int64 = 1024 * 1024 * 10
		var backups int = 10
		var level = obs.LEVEL_DEBUG
		var logToConsole bool = true
		obs.InitLog(logFullPath, maxLogSize, backups, level, logToConsole)
	}

	return &HuaweiObsBackend{
		bucket: bucket,
		flags:  flags,
		config: config,
		cap: Capabilities{
			Name:             "obs",
			MaxMultipartSize: 5 * 1024 * 1024 * 1024,
		},
		client: obsClient,
	}, nil
}

func mapHuaweiObsError(err error) error {
	if obsError, ok := err.(obs.ObsError); ok {
		switch obsError.Code {
		case "BucketRegionError":
			// don't need to log anything, we should detect region after
			return err
		case "NoSuchBucket":
			return syscall.ENXIO
		case "BucketAlreadyOwnedByYou":
			return fuse.EEXIST
		}

		if reqErr, ok := err.(obs.ObsError); ok {
			// A service error occurred
			err = mapHttpError(reqErr.BaseModel.StatusCode)
			if err != nil {
				return err
			} else {
				s3Log.Errorf("http=%v %v s3=%v request=%v\n",
					reqErr.BaseModel.StatusCode, obsError.Message,
					obsError.Code, reqErr.BaseModel.RequestId)
				return reqErr
			}
		} else {
			s3Log.Errorf("code=%v msg=%v, err=%v\n", obsError.Code, obsError.Message, obsError.Error())
			return obsError
		}
	} else {
		return err
	}
}

// Init Implement StorageBackend interface method
func (h *HuaweiObsBackend) Init(key string) (err error) {
	output, err := h.client.HeadBucket(h.bucket)
	if err != nil {
		s3Log.Errorf("Bucket doesn't exist:%s\n", output.RequestId)

	}
	return err
}

// Capabilities Implement StorageBackend interface method
func (h *HuaweiObsBackend) Capabilities() *Capabilities {
	return &h.cap
}

// Bucket Implement StorageBackend interface method
func (h *HuaweiObsBackend) Bucket() string {
	return h.bucket
}

func (h *HuaweiObsBackend) HeadBlob(param *HeadBlobInput) (*HeadBlobOutput, error) {
	input := &obs.GetObjectMetadataInput{}
	input.Bucket = h.bucket
	input.Key = param.Key

	res, err := h.client.GetObjectMetadata(input)
	if err != nil {
		return nil, mapHuaweiObsError(err)
	}

	return &HeadBlobOutput{
		BlobItemOutput: BlobItemOutput{
			Key:          &param.Key,
			ETag:         &res.ETag,
			Size:         uint64(res.ContentLength),
			LastModified: &res.LastModified,
			StorageClass: (*string)(&res.StorageClass),
		},
		ContentType: &res.ContentType,
		Metadata:    h.ConvertMetadata(res.Metadata),
	}, nil
}

func (h *HuaweiObsBackend) ListBlobs(param *ListBlobsInput) (*ListBlobsOutput, error) {
	var maxKeys int

	if param.MaxKeys != nil {
		maxKeys = int(*param.MaxKeys)
	}

	input := &obs.ListObjectsInput{
		ListObjsInput: obs.ListObjsInput{
			Prefix:    *param.Prefix,
			MaxKeys:   maxKeys,
			Delimiter: *param.Delimiter,
		},
		Bucket: h.bucket,
	}

	res, err := h.client.ListObjects(input)

	if err != nil {
		return nil, mapHuaweiObsError(err)
	}

	prefixes := make([]BlobPrefixOutput, 0)
	items := make([]BlobItemOutput, 0)

	for k, _ := range res.CommonPrefixes {
		prefixes = append(prefixes, BlobPrefixOutput{Prefix: &res.CommonPrefixes[k]})
	}

	for k, _ := range res.Contents {
		items = append(items, BlobItemOutput{
			Key:          &res.Contents[k].Key,
			ETag:         &res.Contents[k].ETag,
			LastModified: &res.Contents[k].LastModified,
			Size:         uint64(res.Contents[k].Size),
			StorageClass: (*string)(&res.Contents[k].StorageClass),
		})
	}

	return &ListBlobsOutput{
		Prefixes:              prefixes,
		Items:                 items,
		NextContinuationToken: &res.NextMarker,
		IsTruncated:           res.IsTruncated,
		RequestId:             res.RequestId,
	}, nil
}

func (h *HuaweiObsBackend) DeleteBlob(param *DeleteBlobInput) (*DeleteBlobOutput, error) {
	input := &obs.DeleteObjectInput{}
	input.Bucket = h.bucket
	input.Key = param.Key
	_, err := h.client.DeleteObject(input)
	if err != nil {
		return nil, mapAwsError(err)
	}
	return &DeleteBlobOutput{}, nil
}

func (h *HuaweiObsBackend) DeleteBlobs(param *DeleteBlobsInput) (*DeleteBlobsOutput, error) {

	input := &obs.DeleteObjectsInput{}
	input.Bucket = h.bucket

	num_objs := len(param.Items)
	input.Objects = make([]obs.ObjectToDelete, num_objs)
	for i, _ := range param.Items {
		input.Objects[i] = obs.ObjectToDelete{Key: param.Items[i]}
	}
	res, err := h.client.DeleteObjects(input)
	if err != nil {
		return nil, mapAwsError(err)
	}

	return &DeleteBlobsOutput{res.RequestId}, nil
}

func (h *HuaweiObsBackend) RenameBlob(param *RenameBlobInput) (*RenameBlobOutput, error) {
	return nil, syscall.ENOTSUP
}

func (h *HuaweiObsBackend) CopyBlob(param *CopyBlobInput) (*CopyBlobOutput, error) {
	var metadataDirective obs.MetadataDirectiveType
	if param.Metadata != nil {
		metadataDirective = obs.ReplaceMetadata
	}

	input := &obs.CopyObjectInput{}
	input.Bucket = h.bucket
	input.Key = param.Destination
	input.CopySourceBucket = h.bucket
	input.CopySourceKey = param.Source
	input.MetadataDirective = metadataDirective

	res, err := h.client.CopyObject(input)
	if err != nil {
		return nil, err
	}
	return &CopyBlobOutput{res.RequestId}, nil
}

func (h *HuaweiObsBackend) GetBlob(param *GetBlobInput) (*GetBlobOutput, error) {
	input := &obs.GetObjectInput{}
	input.Bucket = h.bucket
	input.Key = param.Key

	if param.Start != 0 || param.Count != 0 {
		if param.Count != 0 {
			input.RangeStart = int64(param.Start)
			input.RangeEnd = int64(param.Start + param.Count - 1)
		} else {
			input.RangeStart = int64(param.Start)
		}
	}

	res, err := h.client.GetObject(input)
	if err != nil {
		return nil, mapHuaweiObsError(err)
	}

	return &GetBlobOutput{
		HeadBlobOutput: HeadBlobOutput{
			BlobItemOutput: BlobItemOutput{
				Key:          &param.Key,
				ETag:         &res.GetObjectMetadataOutput.ETag,
				Size:         uint64(res.GetObjectMetadataOutput.ContentLength),
				LastModified: &res.GetObjectMetadataOutput.LastModified,
				StorageClass: (*string)(&res.GetObjectMetadataOutput.StorageClass),
			},
			ContentType: &res.GetObjectMetadataOutput.ContentType,
			Metadata:    h.ConvertMetadata(res.GetObjectMetadataOutput.Metadata),
		},
		Body:      res.Body,
		RequestId: res.RequestId,
	}, nil
}

func (h *HuaweiObsBackend) PutBlob(param *PutBlobInput) (*PutBlobOutput, error) {
	storageClass := obs.StorageClassStandard
	if param.Size != nil && *param.Size < 128*1024 && h.config.StorageClass == "STANDARD" {
		storageClass = obs.StorageClassStandard
	}

	s3Log.Debugf("PutBlob: %v", param)

	input := &obs.PutObjectInput{}
	input.Bucket = h.bucket
	input.Key = param.Key
	input.StorageClass = storageClass
	if param.ContentType != nil {
		input.ContentType = *param.ContentType
	}
	input.Body = param.Body

	if h.config.ACL != "" {
		input.ACL = obs.AclType(h.config.ACL)
	}

	res, err := h.client.PutObject(input)

	if err != nil {
		return nil, mapHuaweiObsError(err)
	}

	return &PutBlobOutput{
		ETag:         &res.ETag,
		LastModified: h.getDate(res.BaseModel.ResponseHeaders),
		StorageClass: (*string)(&res.StorageClass),
		RequestId:    res.RequestId,
	}, nil
}

func (h *HuaweiObsBackend) MultipartBlobBegin(param *MultipartBlobBeginInput) (*MultipartBlobCommitInput, error) {
	input := &obs.InitiateMultipartUploadInput{}
	input.Bucket = h.bucket
	input.Key = param.Key

	if h.config.ACL != "" {
		input.ACL = obs.AclType(h.config.ACL)
	}

	res, err := h.client.InitiateMultipartUpload(input)
	if err != nil {
		s3Log.Errorf("CreateMultipartUpload %v = %v", param.Key, err)
		return nil, mapAwsError(err)
	}
	return &MultipartBlobCommitInput{
		Key:      &param.Key,
		Metadata: metadataToLower(param.Metadata),
		UploadId: &res.UploadId,
		Parts:    make([]*string, 10000), // at most 10K parts
	}, nil
}

func (h *HuaweiObsBackend) MultipartBlobAdd(param *MultipartBlobAddInput) (*MultipartBlobAddOutput, error) {
	en := &param.Commit.Parts[param.PartNumber-1]
	atomic.AddUint32(&param.Commit.NumParts, 1)

	input := &obs.UploadPartInput{}
	input.Bucket = h.bucket
	input.Key = *param.Commit.Key
	input.UploadId = *param.Commit.UploadId
	input.PartNumber = int(param.PartNumber)
	input.Body = param.Body
	s3Log.Debugf("MultipartBlobAdd: %v", input)

	res, err := h.client.UploadPart(input)
	if err != nil {
		return nil, mapAwsError(err)
	}
	if *en != nil {
		panic(fmt.Sprintf("etags for part %v already set: %v", param.PartNumber, **en))
	}
	*en = &res.ETag

	return &MultipartBlobAddOutput{res.RequestId}, nil
}

func (h *HuaweiObsBackend) MultipartBlobAbort(param *MultipartBlobCommitInput) (*MultipartBlobAbortOutput, error) {
	input := &obs.AbortMultipartUploadInput{}
	input.Bucket = h.bucket
	input.Key = *param.Key
	input.UploadId = *param.UploadId
	res, err := h.client.AbortMultipartUpload(input)
	if err != nil {
		return nil, mapHuaweiObsError(err)
	}
	return &MultipartBlobAbortOutput{res.RequestId}, nil
}

func (h *HuaweiObsBackend) MultipartBlobCommit(param *MultipartBlobCommitInput) (*MultipartBlobCommitOutput, error) {
	input := &obs.CompleteMultipartUploadInput{}
	input.Bucket = h.bucket
	input.Key = *param.Key
	input.UploadId = *param.UploadId
	input.Parts = make([]obs.Part, param.NumParts)
	for i := uint32(0); i < param.NumParts; i++ {
		input.Parts[i] = obs.Part{
			ETag:       *param.Parts[i],
			PartNumber: int(i + 1),
		}
	}

	res, err := h.client.CompleteMultipartUpload(input)
	if err != nil {
		return nil, mapHuaweiObsError(err)
	}
	s3Log.Debugf("MultipartBlobCommit: %v", res)

	return &MultipartBlobCommitOutput{
		ETag:         &res.ETag,
		LastModified: h.getDate(res.BaseModel.ResponseHeaders),
		RequestId:    res.RequestId,
	}, nil
}

func (h *HuaweiObsBackend) MultipartExpire(param *MultipartExpireInput) (*MultipartExpireOutput, error) {
	mpu, err := h.client.ListMultipartUploads(&obs.ListMultipartUploadsInput{
		Bucket: h.bucket,
	})
	if err != nil {
		return nil, mapHuaweiObsError(err)
	}
	s3Log.Debugf("MultipartExpire mpu: %v", mpu)

	now := time.Now()
	for _, upload := range mpu.Uploads {
		expireTime := upload.Initiated.Add(48 * time.Hour)

		if !expireTime.After(now) {
			params := &obs.AbortMultipartUploadInput{
				Bucket:   h.bucket,
				Key:      upload.Key,
				UploadId: upload.UploadId,
			}
			resp, err := h.client.AbortMultipartUpload(params)
			s3Log.Debugf("MultipartExpire resp: %v", resp)

			if mapHuaweiObsError(err) == syscall.EACCES {
				break
			}
		} else {
			s3Log.Debugf("Keeping MPU Key=%v Id=%v", upload.Key, upload.UploadId)
		}
	}

	return &MultipartExpireOutput{}, nil
}

func (h *HuaweiObsBackend) RemoveBucket(param *RemoveBucketInput) (*RemoveBucketOutput, error) {
	return &RemoveBucketOutput{}, nil
}

func (h *HuaweiObsBackend) MakeBucket(param *MakeBucketInput) (*MakeBucketOutput, error) {
	return &MakeBucketOutput{}, nil
}

func (h *HuaweiObsBackend) Delegate() interface{} {
	return h
}

func stringPtr(s string) *string {
	return &s
}

func (h *HuaweiObsBackend) ConvertMetadata(source map[string]string) map[string]*string {
	result := make(map[string]*string, len(source))

	for key, value := range source {
		result[key] = stringPtr(value)
	}

	return result
}

func (h *HuaweiObsBackend) getDate(headers map[string][]string) *time.Time {
	var lastModified time.Time
	if dateStr, ok := headers["Date"]; ok && len(dateStr) > 0 {
		lastModified, _ = time.Parse(time.RFC1123, dateStr[0])
	}

	return &lastModified
}
