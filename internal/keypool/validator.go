package keypool

import (
	"context"
	"fmt"
	"gpt-load/internal/channel"
	"gpt-load/internal/config"
	"gpt-load/internal/encryption"
	"gpt-load/internal/models"
	"gpt-load/internal/proxypool"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
	"go.uber.org/dig"
	"gorm.io/gorm"
)

// KeyTestResult holds the validation result for a single key.
type KeyTestResult struct {
	KeyValue string `json:"key_value"`
	IsValid  bool   `json:"is_valid"`
	Error    string `json:"error,omitempty"`
}

// KeyValidator provides methods to validate API keys.
type KeyValidator struct {
	DB              *gorm.DB
	channelFactory  *channel.Factory
	SettingsManager *config.SystemSettingsManager
	keypoolProvider *KeyProvider
	encryptionSvc   encryption.Service
	proxyPool       *proxypool.Manager
}

type KeyValidatorParams struct {
	dig.In
	DB              *gorm.DB
	ChannelFactory  *channel.Factory
	SettingsManager *config.SystemSettingsManager
	KeypoolProvider *KeyProvider
	EncryptionSvc   encryption.Service
	ProxyPool       *proxypool.Manager
}

// NewKeyValidator creates a new KeyValidator.
func NewKeyValidator(params KeyValidatorParams) *KeyValidator {
	return &KeyValidator{
		DB:              params.DB,
		channelFactory:  params.ChannelFactory,
		SettingsManager: params.SettingsManager,
		keypoolProvider: params.KeypoolProvider,
		encryptionSvc:   params.EncryptionSvc,
		proxyPool:       params.ProxyPool,
	}
}

// ValidateSingleKey performs a validation check on a single API key.
func (s *KeyValidator) ValidateSingleKey(key *models.APIKey, group *models.Group) (bool, error) {
	if group.EffectiveConfig.AppUrl == "" {
		group.EffectiveConfig = s.SettingsManager.GetEffectiveConfig(group.Config)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(group.EffectiveConfig.KeyValidationTimeoutSeconds)*time.Second)
	defer cancel()

	ch, err := s.channelFactory.GetChannel(group)
	if err != nil {
		return false, fmt.Errorf("failed to get channel for group %s: %w", group.Name, err)
	}

	var lastProxyErr error
	for attempt := 0; attempt < proxyPoolValidationAttempts(group); attempt++ {
		client := ch.GetHTTPClient()
		proxySelection, proxyErr := s.proxyPool.Select(group, key.ID)
		if proxyErr != nil {
			return false, proxyErr
		}
		if proxySelection.FromPool {
			client = s.channelFactory.GetClientForGroup(group, proxySelection.URL, false)
		}

		isValid, validationErr := validateKeyWithClient(ctx, ch, key, group, client)
		if validationErr == nil && proxySelection.FromPool {
			s.proxyPool.MarkSuccess(group.ID, proxySelection.URL)
		}
		if validationErr != nil && proxySelection.FromPool && isValidationProxyError(validationErr, proxySelection.URL) {
			s.proxyPool.MarkFailure(group.ID, key.ID, proxySelection.URL, proxySelection.CooldownSeconds)
			lastProxyErr = validationErr
			continue
		}

		var errorMsg string
		if !isValid && validationErr != nil {
			errorMsg = validationErr.Error()
		}
		s.keypoolProvider.UpdateStatus(key, group, isValid, errorMsg)

		if !isValid {
			logrus.WithFields(logrus.Fields{
				"error":    validationErr,
				"key_id":   key.ID,
				"group_id": group.ID,
			}).Debug("Key validation failed")
			return false, validationErr
		}

		logrus.WithFields(logrus.Fields{
			"key_id":   key.ID,
			"is_valid": isValid,
		}).Debug("Key validation successful")

		return true, nil
	}

	if lastProxyErr != nil {
		logrus.WithFields(logrus.Fields{
			"error":    lastProxyErr,
			"key_id":   key.ID,
			"group_id": group.ID,
		}).Debug("Key validation failed because all proxy pool entries failed")
		return false, fmt.Errorf("all proxy pool entries failed validation: %w", lastProxyErr)
	}

	logrus.WithFields(logrus.Fields{
		"key_id": key.ID,
	}).Debug("Key validation successful")

	return true, nil
}

func validateKeyWithClient(ctx context.Context, ch channel.ChannelProxy, key *models.APIKey, group *models.Group, client *http.Client) (bool, error) {
	if validator, ok := ch.(channel.ClientValidator); ok {
		return validator.ValidateKeyWithClient(ctx, key, group, client)
	}
	return ch.ValidateKey(ctx, key, group)
}

func isValidationProxyError(err error, proxyURL string) bool {
	return proxypool.IsProxyTransportError(err, proxyURL) || proxypool.IsRecoverableErrorMessage(err.Error())
}

func proxyPoolValidationAttempts(group *models.Group) int {
	groupConfig, err := models.DecodeGroupConfig(group.Config)
	if err != nil || groupConfig.ProxyPool == nil || len(groupConfig.ProxyPool.Entries()) == 0 {
		return 1
	}
	if selectable := len(groupConfig.ProxyPool.SelectableEntries()); selectable > 0 {
		return selectable
	}
	return 1
}

// TestMultipleKeys performs a synchronous validation for a list of key values within a specific group.
func (s *KeyValidator) TestMultipleKeys(group *models.Group, keyValues []string) ([]KeyTestResult, error) {
	results := make([]KeyTestResult, len(keyValues))

	// Generate hashes for all key values
	var keyHashes []string
	for _, keyValue := range keyValues {
		keyHash := s.encryptionSvc.Hash(keyValue)
		if keyHash == "" {
			continue
		}
		keyHashes = append(keyHashes, keyHash)
	}

	// Find which of the provided keys actually exist in the database for this group
	var existingKeys []models.APIKey
	if len(keyHashes) > 0 {
		if err := s.DB.Where("group_id = ? AND key_hash IN ?", group.ID, keyHashes).Find(&existingKeys).Error; err != nil {
			return nil, fmt.Errorf("failed to query keys from DB: %w", err)
		}
	}

	// Create a map of key_hash to APIKey for quick lookup
	existingKeyMap := make(map[string]models.APIKey)
	for _, k := range existingKeys {
		existingKeyMap[k.KeyHash] = k
	}

	for i, kv := range keyValues {
		keyHash := s.encryptionSvc.Hash(kv)
		apiKey, exists := existingKeyMap[keyHash]
		if !exists {
			results[i] = KeyTestResult{
				KeyValue: kv,
				IsValid:  false,
				Error:    "Key does not exist in this group or has been removed.",
			}
			continue
		}

		apiKey.KeyValue = kv

		isValid, validationErr := s.ValidateSingleKey(&apiKey, group)

		results[i] = KeyTestResult{
			KeyValue: kv,
			IsValid:  isValid,
			Error:    "",
		}
		if validationErr != nil {
			results[i].Error = validationErr.Error()
		}
	}

	return results, nil
}
