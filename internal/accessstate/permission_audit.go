package accessstate

import "slices"

// PermissionAudits returns detached committed checked-permission receipts for
// trusted local inspection. It is not complete management history, a model
// diagnostic or current authority; absent legacy audit facts remain absent.
func (s *Store) PermissionAudits() []PermissionReceipt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]PermissionReceipt, 0)
	for _, receipt := range s.permissionReceipts {
		if receipt.Audit == nil {
			continue
		}
		audit := *receipt.Audit
		audit.Services = slices.Clone(audit.Services)
		if audit.RequestLimits != nil {
			rates := slices.Clone(*audit.RequestLimits)
			audit.RequestLimits = &rates
		}
		receipt.Audit = &audit
		result = append(result, receipt)
	}
	return result
}
