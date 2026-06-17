/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"

	"github.com/pluralsh/controller-reconcile-helper/pkg/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Scope[T client.Object] interface {
	PatchObject() error
}

type DefaultScope[T client.Object] struct {
	client      client.Client
	object      T
	ctx         context.Context
	patchHelper *patch.Helper
}

func (in *DefaultScope[T]) PatchObject() error {
	return in.patchHelper.Patch(in.ctx, in.object)
}

func NewDefaultScope[T client.Object](ctx context.Context, client client.Client, object T) (Scope[T], error) {
	helper, err := patch.NewHelper(object, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create patch helper: %w", err)
	}

	return &DefaultScope[T]{
		client:      client,
		object:      object,
		ctx:         ctx,
		patchHelper: helper,
	}, nil
}
