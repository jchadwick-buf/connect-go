// Copyright 2021-2023 Buf Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package connect_test

import (
	"errors"
	"fmt"

	"github.com/bufbuild/connect-go"
)

func ExampleError_Message() {
	err := fmt.Errorf(
		"another: %w",
		connect.NewError(connect.CodeUnavailable, errors.New("failed to foo")),
	)
	if connectErr := (&connect.Error{}); errors.As(err, &connectErr) {
		fmt.Println("underlying error message:", connectErr.Message())
	}

	// Output:
	// underlying error message: failed to foo
}
