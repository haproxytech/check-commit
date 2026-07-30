// Copyright 2019 HAProxy Technologies LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package version

import (
	"errors"
	"runtime/debug"
	"strings"
)

var (
	Repo       = ""
	Version    = "dev"
	Tag        = "dev"
	CommitDate = ""
)

var ErrBuildDataNotReadable = errors.New("not able to read build data")

func Set() error {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return ErrBuildDataNotReadable
	}
	Repo = buildInfo.Main.Path
	CommitDate = get(buildInfo, "vcs.time")
	commit := get(buildInfo, "vcs.revision")
	if len(commit) > 8 {
		commit = commit[:8]
	}

	var dirty string
	if get(buildInfo, "vcs.modified") == "true" {
		dirty = ".dirty"
	}
	Tag = strings.Replace(buildInfo.Main.Version, "(devel)", "dev", 1)
	// Proxy builds carry no VCS metadata: show the bare module version.
	Version = Tag + dirty
	if commit != "" {
		Version = Tag + "." + commit + dirty
	}

	return nil
}

func get(buildInfo *debug.BuildInfo, key string) string {
	for _, setting := range buildInfo.Settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return ""
}
