// Copyright 2026 PolitePixels Limited
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

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package app

import (
	"bytes"
	"errors"
	"go/types"
	"log/slog"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/symtab"
)

func TestUseSymbolProvidersReturnsARejectedTypesPackage(t *testing.T) {
	service := NewService()
	err := service.UseSymbolProviders(staleLayoutProvider())
	require.ErrorIs(t, err, symtab.ErrTypesPackageLayout)
	require.ErrorContains(t, err, `"layouthost"`)
	require.True(t, service.HasRegisteredPackage("layouthost"), "the registry is installed although a types package was rejected")
}

type undecodableTypesProvider struct{}

var errUndecodableTypes = errors.New("export data truncated")

func (undecodableTypesProvider) Exports() symtab.SymbolExports {
	return symtab.SymbolExports{"math": {"Pi": reflect.ValueOf(math.Pi)}}
}

func (undecodableTypesProvider) TypesPackages() map[string]*types.Package { return nil }

func (undecodableTypesProvider) TypesPackagesLoadError() error { return errUndecodableTypes }

func TestUseSymbolProvidersReturnsAProvidersDecodeFailure(t *testing.T) {
	service := NewService()
	err := service.UseSymbolProviders(undecodableTypesProvider{})
	require.ErrorIs(t, err, errUndecodableTypes)
	require.True(t, service.HasRegisteredPackage("math"), "the provider's symbols are still registered")

	_, err = NewRestrictedInterpreterWithProvider(RestrictedConfig{Imports: []string{"math"}}, undecodableTypesProvider{})
	require.ErrorIs(t, err, errUndecodableTypes, "a restricted interpreter fails closed")
	require.ErrorIs(t, err, ErrInvalidRestrictedConfig)
}

func TestARejectedProviderIsReportedThroughTheFirstCallsLogger(t *testing.T) {
	var fallback bytes.Buffer
	service := NewService(WithSymbolProvider(undecodableTypesProvider{}), WithLogger(slog.New(slog.NewTextHandler(&fallback, nil))))
	require.Empty(t, fallback.String(), "construction logs nothing")

	var bound bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&bound, nil)))
	_, err := service.Eval(ctx, "1 + 1")
	require.ErrorIs(t, err, errUndecodableTypes, "the first call that takes a context returns the rejection")
	require.Contains(t, bound.String(), "could not all be registered", "and logs it through the context's logger")
	require.Empty(t, fallback.String(), "a logger in the context wins over the WithLogger option")

	result, err := service.Eval(t.Context(), "1 + 1")
	require.NoError(t, err, "the rejection is reported once")
	require.EqualValues(t, 2, result)
	require.Empty(t, fallback.String())
}

func TestTheWithLoggerOptionIsTheFallbackForAContextWithoutALogger(t *testing.T) {
	var fallback bytes.Buffer
	service := NewService(WithSymbolProvider(undecodableTypesProvider{}), WithLogger(slog.New(slog.NewTextHandler(&fallback, nil))))
	_, err := service.Eval(t.Context(), "1 + 1")
	require.ErrorIs(t, err, errUndecodableTypes)
	require.Contains(t, fallback.String(), "could not all be registered")
}
