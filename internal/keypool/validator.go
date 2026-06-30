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

	if entries, cooldownSeconds, ok, err := proxyPoolValidationEntries(group); err != nil {
		return false, err
	} else if ok {
		return s.validateSingleKeyWithProxyPool(ctx, ch, key, group, entries, cooldownSeconds)
	}

	return s.validateSingleKeyWithClient(ctx, ch, key, group, ch.GetHTTPClient())
}

func (s *KeyValidator) validateSingleKeyWithProxyPool(
	ctx context.Context,
	ch channel.ChannelProxy,
	key *models.APIKey,
	group *models.Group,
	entries []models.ProxyPoolItem,
	cooldownSeconds int,
) (bool, error) {
	var lastProxyErr error
	for _, entry := range entries {
		client := s.channelFactory.GetClientForGroup(group, entry.URL, false)
		isValid, validationErr := validateKeyWithClient(ctx, ch, key, group, client)
		if validationErr == nil {
			s.proxyPool.MarkSuccess(group.ID, entry.URL)
		}
		if validationErr != nil && isValidationProxyError(validationErr, entry.URL) {
			s.proxyPool.MarkFailure(group.ID, key.ID, entry.URL, cooldownSeconds)
			lastProxyErr = validationErr
			logrus.WithFields(logrus.Fields{
				"error":     validationErr,
				"key_id":    key.ID,
				"group_id":  group.ID,
				"proxy_url": entry.URL,
			}).Debug("Key validation proxy failed, trying next proxy pool entry")
			continue
		}

		return s.finishValidation(key, group, isValid, validationErr)
	}

	if lastProxyErr != nil {
		logrus.WithFields(logrus.Fields{
			"error":    lastProxyErr,
			"key_id":   key.ID,
			"group_id": group.ID,
		}).Debug("Key validation failed because all proxy pool entries failed")
		return false, fmt.Errorf("all proxy pool entries failed validation: %w", lastProxyErr)
	}

	return false, fmt.Errorf("no enabled proxy in group proxy pool")
}

func (s *KeyValidator) validateSingleKeyWithClient(
	ctx context.Context,
	ch channel.ChannelProxy,
	key *models.APIKey,
	group *models.Group,
	client *http.Client,
) (bool, error) {
	isValid, validationErr := validateKeyWithClient(ctx, ch, key, group, client)
	return s.finishValidation(key, group, isValid, validationErr)
}

func (s *KeyValidator) finishValidation(key *models.APIKey, group *models.Group, isValid bool, validationErr error) (bool, error) {
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

func validateKeyWithClient(ctx context.Context, ch channel.ChannelProxy, key *models.APIKey, group *models.Group, client *http.Client) (bool, error) {
	if validator, ok := ch.(channel.ClientValidator); ok {
		return validator.ValidateKeyWithClient(ctx, key, group, client)
	}
	return ch.ValidateKey(ctx, key, group)
}

func isValidationProxyError(err error, proxyURL string) bool {
	return proxypool.IsProxyTransportError(err, proxyURL) || proxypool.IsRecoverableErrorMessage(err.Error())
}

func proxyPoolValidationEntries(group *models.Group) ([]models.ProxyPoolItem, int, bool, error) {
	groupConfig, err := models.DecodeGroupConfig(group.Config)
	if err != nil || groupConfig.ProxyPool == nil || len(groupConfig.ProxyPool.Entries()) == 0 {
		return nil, 0, false, err
	}
	entries := groupConfig.ProxyPool.SelectableEntries()
	if len(entries) == 0 {
		return nil, 0, true, nil
	}
	cooldownSeconds := groupConfig.ProxyPool.AutoEnableIntervalSeconds
	if cooldownSeconds <= 0 {
		cooldownSeconds = groupConfig.ProxyPool.CooldownSeconds
	}
	if cooldownSeconds <= 0 {
		cooldownSeconds = 60
	}
	return entries, cooldownSeconds, true, nil
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
