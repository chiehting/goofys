// Copyright 2019 Databricks
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

package common

import (
	"os"
)

type HuaweiObsConfig struct {
	Profile   string
	AccessKey string
	SecretKey string
	Endpoint  string

	// RoleArn         string
	// RoleExternalId  string
	// RoleSessionName string
	// StsEndpoint     string

	// RequesterPays bool
	Region string
	// RegionSet     bool

	StorageClass string

	// UseSSE     bool
	// UseKMS     bool
	// KMSKeyID   string
	// SseC       string
	// SseCDigest string
	ACL string

	// Subdomain bool

	// Credentials *credentials.Credentials
	// Session     *session.Session

	// BucketOwner string
}

// Init initializes the HuaweiObsConfig with default values if not set.
func (c *HuaweiObsConfig) Init() *HuaweiObsConfig {
	c.Region = os.Getenv("HUAWEI_REGION")
	if c.Region == "" {
		c.Region = "cn-east-3"
	}
	if c.StorageClass == "" {
		c.StorageClass = "STANDARD"
	}

	c.AccessKey = os.Getenv("HUAWEI_ACCESS_KEY_ID")
	c.SecretKey = os.Getenv("HUAWEI_SECRET_ACCESS_KEY")
	c.SecretKey = os.Getenv("HUAWEI_SECRET_ACCESS_KEY")

	return c
}
