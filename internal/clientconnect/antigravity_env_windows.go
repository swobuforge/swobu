//go:build windows

package clientconnect

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var sendMessageTimeoutW = windows.NewLazySystemDLL("user32.dll").NewProc("SendMessageTimeoutW")

const windowsMachineEnvironment = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`

func inspectAntigravityEnvironment(_ *Service, target Target) (antigravityEnvironmentPlan, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	keyExists := err == nil
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return antigravityEnvironmentPlan{}, err
	}
	var endpoint, apiKey string
	endpointExists, apiKeyExists, userEndpointDefined, userAPIKeyDefined := false, false, false, false
	if keyExists {
		defer key.Close()
		var endpointErr error
		endpoint, _, endpointErr = key.GetStringValue("GOOGLE_GEMINI_BASE_URL")
		userEndpointDefined = endpointErr == nil
		endpointExists = endpointErr == nil
		if endpointErr != nil && !errors.Is(endpointErr, registry.ErrNotExist) {
			return antigravityEnvironmentPlan{}, endpointErr
		}
		var apiKeyErr error
		apiKey, _, apiKeyErr = key.GetStringValue("GEMINI_API_KEY")
		userAPIKeyDefined = apiKeyErr == nil
		apiKeyExists = apiKeyErr == nil && apiKey != ""
		if apiKeyErr != nil && !errors.Is(apiKeyErr, registry.ErrNotExist) {
			return antigravityEnvironmentPlan{}, apiKeyErr
		}
	}
	// Machine values are effective only when the user locus is absent. Inspect
	// that persistent precedence before deciding whether HKCU must shadow it.
	if !userEndpointDefined || (!apiKeyExists && !userAPIKeyDefined) {
		machineKey, machineErr := registry.OpenKey(registry.LOCAL_MACHINE, windowsMachineEnvironment, registry.QUERY_VALUE)
		if machineErr != nil && !errors.Is(machineErr, registry.ErrNotExist) {
			return antigravityEnvironmentPlan{}, machineErr
		}
		if machineErr == nil {
			defer machineKey.Close()
			if !userEndpointDefined {
				machineEndpoint, _, valueErr := machineKey.GetStringValue("GOOGLE_GEMINI_BASE_URL")
				if valueErr != nil && !errors.Is(valueErr, registry.ErrNotExist) {
					return antigravityEnvironmentPlan{}, valueErr
				}
				if valueErr == nil {
					endpoint, endpointExists = machineEndpoint, true
				}
			}
			if !apiKeyExists && !userAPIKeyDefined {
				machineAPIKey, _, valueErr := machineKey.GetStringValue("GEMINI_API_KEY")
				if valueErr != nil && !errors.Is(valueErr, registry.ErrNotExist) {
					return antigravityEnvironmentPlan{}, valueErr
				}
				apiKeyExists = valueErr == nil && machineAPIKey != ""
			}
		}
	}
	endpointChanges := semanticChange("endpoint", endpoint, endpointExists, target.WorkspaceURL())
	changes := append([]Change(nil), endpointChanges...)
	if !apiKeyExists {
		changes = append(changes, Change{Field: "API key fallback", After: "swobu-local"})
	}
	plan := antigravityEnvironmentPlan{
		configPaths: []string{`HKCU\Environment`},
		changes:     changes,
	}
	if len(changes) == 0 {
		return plan, nil
	}
	plan.apply = func(context.Context) error {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer key.Close()
		if !apiKeyExists {
			if err := key.SetStringValue("GEMINI_API_KEY", "swobu-local"); err != nil {
				return err
			}
		}
		if len(endpointChanges) != 0 {
			if err := key.SetStringValue("GOOGLE_GEMINI_BASE_URL", target.WorkspaceURL()); err != nil {
				return err
			}
		}
		return broadcastAntigravityEnvironmentChange()
	}
	return plan, nil
}

func broadcastAntigravityEnvironmentChange() error {
	environment, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return err
	}
	var result uintptr
	const (
		hwndBroadcast    = 0xffff
		wmSettingChange  = 0x001a
		smtoAbortIfHung  = 0x0002
		broadcastTimeout = 5000
	)
	returnValue, _, callErr := sendMessageTimeoutW.Call(
		hwndBroadcast,
		wmSettingChange,
		0,
		uintptr(unsafe.Pointer(environment)),
		smtoAbortIfHung,
		broadcastTimeout,
		uintptr(unsafe.Pointer(&result)),
	)
	if returnValue == 0 {
		return fmt.Errorf("notify Windows of Antigravity environment change: %w", callErr)
	}
	return nil
}
