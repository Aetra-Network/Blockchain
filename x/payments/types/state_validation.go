package types

import (
	"errors"
)

func validateChannels(channels []ChannelRecord) error {
	seen := make(map[string]struct{}, len(channels))
	var previous string
	for i, channel := range channels {
		channel = channel.Normalize()
		if err := channel.Validate(); err != nil {
			return err
		}
		if _, found := seen[channel.ChannelID]; found {
			return errors.New("payments duplicate channel")
		}
		seen[channel.ChannelID] = struct{}{}
		if i > 0 && previous >= channel.ChannelID {
			return errors.New("payments channels must be sorted canonically")
		}
		previous = channel.ChannelID
	}
	return nil
}

func validateEdges(channels []ChannelRecord, edges []ChannelEdge) error {
	channelByID := channelMap(channels)
	seen := make(map[string]struct{}, len(edges))
	var previous string
	for i, edge := range edges {
		edge = edge.Normalize()
		if err := edge.Validate(); err != nil {
			return err
		}
		channel, found := channelByID[edge.ChannelID]
		if !found {
			return errors.New("payments routing edge references unknown channel")
		}
		if channel.Status != ChannelStatusOpen {
			return errors.New("payments routing edge references non-open channel")
		}
		if !containsString(channel.Participants, edge.From) || !containsString(channel.Participants, edge.To) {
			return errors.New("payments routing edge endpoints must be channel participants")
		}
		key := edgeKey(edge)
		if _, found := seen[key]; found {
			return errors.New("payments duplicate routing edge")
		}
		seen[key] = struct{}{}
		if i > 0 && previous >= key {
			return errors.New("payments routing edges must be sorted canonically")
		}
		previous = key
	}
	return nil
}

func validateSettlements(channels []ChannelRecord, settlements []SettlementRecord) error {
	channelByID := channelMap(channels)
	seen := make(map[string]struct{}, len(settlements))
	var previous string
	for i, settlement := range settlements {
		settlement = settlement.Normalize()
		channel, found := channelByID[settlement.ChannelID]
		if !found {
			return errors.New("payments settlement references unknown channel")
		}
		if err := settlement.ValidateForChannel(channel); err != nil {
			return err
		}
		if _, found := seen[settlement.ChannelID]; found {
			return errors.New("payments duplicate settlement")
		}
		seen[settlement.ChannelID] = struct{}{}
		if i > 0 && previous >= settlement.ChannelID {
			return errors.New("payments settlements must be sorted canonically")
		}
		previous = settlement.ChannelID
	}
	return nil
}

func validateBatches(channels []ChannelRecord, batches []SettlementBatch) error {
	channelByID := channelMap(channels)
	seen := make(map[string]struct{}, len(batches))
	var previous string
	for i, batch := range batches {
		batch = batch.Normalize()
		if err := batch.Validate(); err != nil {
			return err
		}
		for _, op := range batch.Operations {
			if _, found := channelByID[op.ChannelID]; !found {
				return errors.New("payments batch references unknown channel")
			}
		}
		if _, found := seen[batch.BatchID]; found {
			return errors.New("payments duplicate batch")
		}
		seen[batch.BatchID] = struct{}{}
		if i > 0 && previous >= batch.BatchID {
			return errors.New("payments batches must be sorted canonically")
		}
		previous = batch.BatchID
	}
	return nil
}

func validateCustodyLocks(channels []ChannelRecord, locks []CustodyLock) error {
	channelByID := channelMap(channels)
	seen := make(map[string]struct{}, len(locks))
	var previous string
	for i, lock := range locks {
		lock = lock.Normalize()
		channel, found := channelByID[lock.ChannelID]
		if !found {
			return errors.New("payments custody lock references unknown channel")
		}
		if channel.Status == ChannelStatusSettled {
			return errors.New("payments settled channel must not retain custody lock")
		}
		if err := lock.ValidateForChannel(channel); err != nil {
			return err
		}
		if _, found := seen[lock.ChannelID]; found {
			return errors.New("payments duplicate custody lock")
		}
		seen[lock.ChannelID] = struct{}{}
		if i > 0 && previous >= lock.ChannelID {
			return errors.New("payments custody locks must be sorted canonically")
		}
		previous = lock.ChannelID
	}
	for _, channel := range channelByID {
		if channel.Status == ChannelStatusSettled {
			continue
		}
		if _, found := seen[channel.ChannelID]; !found {
			return errors.New("payments channel custody lock is required")
		}
	}
	return nil
}

func validateClosedChannelTombstones(channels []ChannelRecord, tombstones []ClosedChannelTombstone) error {
	channelByID := channelMap(channels)
	seen := make(map[string]struct{}, len(tombstones))
	var previous string
	for i, tombstone := range tombstones {
		tombstone = tombstone.Normalize()
		if err := tombstone.Validate(); err != nil {
			return err
		}
		channel, found := channelByID[tombstone.ChannelID]
		if !found {
			return errors.New("payments tombstone references unknown channel")
		}
		if channel.Status != ChannelStatusSettled {
			return errors.New("payments tombstone requires settled channel")
		}
		if tombstone.ChainID != channel.ChainID || tombstone.FinalizedNonce != channel.FinalizedNonce {
			return errors.New("payments tombstone channel domain mismatch")
		}
		if _, found := seen[tombstone.ChannelID]; found {
			return errors.New("payments duplicate closed channel tombstone")
		}
		seen[tombstone.ChannelID] = struct{}{}
		if i > 0 && previous >= tombstone.ChannelID {
			return errors.New("payments closed channel tombstones must be sorted canonically")
		}
		previous = tombstone.ChannelID
	}
	for _, channel := range channelByID {
		if channel.Status != ChannelStatusSettled {
			continue
		}
		if _, found := seen[channel.ChannelID]; !found {
			return errors.New("payments settled channel tombstone is required")
		}
	}
	return nil
}

func validateConditionClaimRecords(channels []ChannelRecord, claims []ConditionClaimRecord) error {
	channelByID := channelMap(channels)
	seenCondition := make(map[string]struct{}, len(claims))
	seenEvidence := make(map[string]struct{}, len(claims))
	var previous string
	for i, claim := range claims {
		claim = claim.Normalize()
		if err := claim.Validate(); err != nil {
			return err
		}
		channel, found := channelByID[claim.ChannelID]
		if !found {
			return errors.New("payments condition claim references unknown channel")
		}
		if claim.ChainID != channel.ChainID {
			return errors.New("payments condition claim channel domain mismatch")
		}
		conditionKey := conditionClaimKey(claim.ChannelID, claim.ConditionID)
		evidenceKey := conditionEvidenceKey(claim.ChannelID, claim.EvidenceHash)
		if _, found := seenCondition[conditionKey]; found {
			return errors.New("payments duplicate condition claim")
		}
		if _, found := seenEvidence[evidenceKey]; found {
			return errors.New("payments duplicate condition evidence claim")
		}
		seenCondition[conditionKey] = struct{}{}
		seenEvidence[evidenceKey] = struct{}{}
		sortKey := conditionKey + "/" + claim.EvidenceHash
		if i > 0 && previous >= sortKey {
			return errors.New("payments condition claims must be sorted canonically")
		}
		previous = sortKey
	}
	return nil
}

func validateValidatorPaymentServices(services []ValidatorPaymentServiceMetadata) error {
	seen := make(map[string]struct{}, len(services))
	var previous string
	for i, metadata := range services {
		metadata = metadata.Normalize()
		if err := metadata.Validate(); err != nil {
			return err
		}
		if metadata.MetadataHash == "" {
			return errors.New("payments validator service metadata hash is required")
		}
		if _, found := seen[metadata.ValidatorAddress]; found {
			return errors.New("payments duplicate validator payment service")
		}
		seen[metadata.ValidatorAddress] = struct{}{}
		if i > 0 && previous >= metadata.ValidatorAddress {
			return errors.New("payments validator services must be sorted canonically")
		}
		previous = metadata.ValidatorAddress
	}
	return nil
}

func validateValidatorWatchRegistrations(services []ValidatorPaymentServiceMetadata, registrations []ValidatorWatchRegistration) error {
	serviceByValidator := make(map[string]ValidatorPaymentServiceMetadata, len(services))
	for _, metadata := range services {
		metadata = metadata.Normalize()
		serviceByValidator[metadata.ValidatorAddress] = metadata
	}
	seen := make(map[string]struct{}, len(registrations))
	var previous string
	for i, registration := range registrations {
		registration = registration.Normalize()
		metadata, found := serviceByValidator[registration.ValidatorAddress]
		if !found {
			return errors.New("payments validator watch registration references unknown service")
		}
		if err := registration.Validate(metadata); err != nil {
			return err
		}
		key := validatorWatchRegistrationKey(registration.ValidatorAddress, registration.Delegator)
		if _, found := seen[key]; found {
			return errors.New("payments duplicate validator watch registration")
		}
		seen[key] = struct{}{}
		if i > 0 && previous >= key {
			return errors.New("payments validator watch registrations must be sorted canonically")
		}
		previous = key
	}
	return nil
}

func validatePaymentFeeMultipliers(schedule PaymentFeeSchedule, multipliers []PaymentFeeMultiplier) error {
	seen := make(map[PaymentFeeClass]struct{}, len(multipliers))
	var previous string
	for i, multiplier := range multipliers {
		multiplier = multiplier.Normalize()
		if err := multiplier.Validate(); err != nil {
			return err
		}
		if multiplier.MultiplierBps > schedule.MaxMultiplierBps {
			return errors.New("payments fee multiplier exceeds schedule maximum")
		}
		if _, found := seen[multiplier.FeeClass]; found {
			return errors.New("payments duplicate fee multiplier")
		}
		seen[multiplier.FeeClass] = struct{}{}
		key := string(multiplier.FeeClass)
		if i > 0 && previous >= key {
			return errors.New("payments fee multipliers must be sorted canonically")
		}
		previous = key
	}
	return nil
}

func validatePaymentFeeCharges(charges []PaymentFeeCharge) error {
	seen := make(map[string]struct{}, len(charges))
	var previous string
	for i, charge := range charges {
		charge = charge.Normalize()
		if err := charge.Validate(); err != nil {
			return err
		}
		if _, found := seen[charge.FeeID]; found {
			return errors.New("payments duplicate fee charge")
		}
		seen[charge.FeeID] = struct{}{}
		if i > 0 && previous >= charge.FeeID {
			return errors.New("payments fee charges must be sorted canonically")
		}
		previous = charge.FeeID
	}
	return nil
}

func validatePaymentFeeRefunds(charges []PaymentFeeCharge, refunds []PaymentFeeRefund) error {
	chargeByID := make(map[string]PaymentFeeCharge, len(charges))
	for _, charge := range charges {
		charge = charge.Normalize()
		chargeByID[charge.FeeID] = charge
	}
	seen := make(map[string]struct{}, len(refunds))
	var previous string
	for i, refund := range refunds {
		refund = refund.Normalize()
		if err := refund.Validate(); err != nil {
			return err
		}
		charge, found := chargeByID[refund.FeeID]
		if !found {
			return errors.New("payments fee refund references unknown charge")
		}
		if !charge.Refunded {
			return errors.New("payments fee refund requires refunded charge marker")
		}
		if refund.Amount != charge.Amount {
			return errors.New("payments fee refund amount must match charge")
		}
		if _, found := seen[refund.RefundID]; found {
			return errors.New("payments duplicate fee refund")
		}
		seen[refund.RefundID] = struct{}{}
		if i > 0 && previous >= refund.RefundID {
			return errors.New("payments fee refunds must be sorted canonically")
		}
		previous = refund.RefundID
	}
	return nil
}

func validateSecurityReserveAllocationHooks(channels []ChannelRecord, hooks []SecurityReserveAllocationHook) error {
	channelByID := channelMap(channels)
	seen := make(map[string]struct{}, len(hooks))
	var previous string
	for i, hook := range hooks {
		hook = hook.Normalize()
		channel, found := channelByID[hook.ChannelID]
		if !found {
			return errors.New("payments security reserve hook channel not found")
		}
		if err := hook.ValidateForChannel(channel); err != nil {
			return err
		}
		if _, found := seen[hook.HookID]; found {
			return errors.New("payments duplicate security reserve hook")
		}
		seen[hook.HookID] = struct{}{}
		if i > 0 && previous >= hook.HookID {
			return errors.New("payments security reserve hooks must be sorted canonically")
		}
		previous = hook.HookID
	}
	return nil
}

func validateSettlementInclusionLatencies(channels []ChannelRecord, records []SettlementInclusionLatency) error {
	seen := make(map[string]struct{}, len(records))
	var previous string
	for i, record := range records {
		record = record.Normalize()
		if err := record.Validate(channels); err != nil {
			return err
		}
		if _, found := seen[record.RecordID]; found {
			return errors.New("payments duplicate settlement inclusion latency")
		}
		seen[record.RecordID] = struct{}{}
		if i > 0 && previous >= record.RecordID {
			return errors.New("payments settlement inclusion latencies must be sorted canonically")
		}
		previous = record.RecordID
	}
	return nil
}

func validateAsyncFinalizationJobs(channels []ChannelRecord, jobs []AsyncFinalizationJob) error {
	channelByID := channelMap(channels)
	seen := make(map[string]struct{}, len(jobs))
	var previous string
	for i, job := range jobs {
		job = job.Normalize()
		if err := job.Validate(); err != nil {
			return err
		}
		if _, found := channelByID[job.ChannelID]; !found {
			return errors.New("payments async finalization references unknown channel")
		}
		if _, found := seen[job.JobID]; found {
			return errors.New("payments duplicate async finalization job")
		}
		seen[job.JobID] = struct{}{}
		if i > 0 && previous >= job.JobID {
			return errors.New("payments async finalization jobs must be sorted canonically")
		}
		previous = job.JobID
	}
	return nil
}

func validateAsyncPromiseExpiryJobs(channels []ChannelRecord, jobs []AsyncPromiseExpiryJob) error {
	channelByID := channelMap(channels)
	seen := make(map[string]struct{}, len(jobs))
	var previous string
	for i, job := range jobs {
		job = job.Normalize()
		if err := job.Validate(); err != nil {
			return err
		}
		channel, found := channelByID[job.ChannelID]
		if !found {
			return errors.New("payments async promise expiry references unknown channel")
		}
		if err := job.Promise.ValidateForChannel(channel); err != nil {
			return err
		}
		if _, found := seen[job.JobID]; found {
			return errors.New("payments duplicate async promise expiry job")
		}
		seen[job.JobID] = struct{}{}
		if i > 0 && previous >= job.JobID {
			return errors.New("payments async promise expiry jobs must be sorted canonically")
		}
		previous = job.JobID
	}
	return nil
}

func validateAsyncSettlementCompletions(channels []ChannelRecord, finalizationJobs []AsyncFinalizationJob, expiryJobs []AsyncPromiseExpiryJob, completions []AsyncSettlementCompletion) error {
	channelByID := channelMap(channels)
	jobIDs := make(map[string]struct{}, len(finalizationJobs)+len(expiryJobs))
	for _, job := range finalizationJobs {
		jobIDs[job.Normalize().JobID] = struct{}{}
	}
	for _, job := range expiryJobs {
		jobIDs[job.Normalize().JobID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(completions))
	var previous string
	for i, completion := range completions {
		completion = completion.Normalize()
		if err := completion.Validate(); err != nil {
			return err
		}
		if _, found := channelByID[completion.ChannelID]; !found {
			return errors.New("payments async completion references unknown channel")
		}
		if _, found := jobIDs[completion.JobID]; !found {
			return errors.New("payments async completion references unknown job")
		}
		if _, found := seen[completion.CompletionID]; found {
			return errors.New("payments duplicate async completion")
		}
		seen[completion.CompletionID] = struct{}{}
		if i > 0 && previous >= completion.CompletionID {
			return errors.New("payments async completions must be sorted canonically")
		}
		previous = completion.CompletionID
	}
	return nil
}

func validatePaymentEvents(channels []ChannelRecord, events []PaymentEvent) error {
	channelByID := channelMap(channels)
	seen := make(map[string]struct{}, len(events))
	openEventByChannel := make(map[string]struct{}, len(channels))
	for _, event := range events {
		event = event.Normalize()
		if err := event.Validate(); err != nil {
			return err
		}
		if _, found := channelByID[event.ChannelID]; !found {
			return errors.New("payments event references unknown channel")
		}
		if _, found := seen[event.EventID]; found {
			return errors.New("payments duplicate event")
		}
		seen[event.EventID] = struct{}{}
		if event.EventType == "channel-open" {
			openEventByChannel[event.ChannelID] = struct{}{}
		}
	}
	for _, channel := range channelByID {
		if _, found := openEventByChannel[channel.ChannelID]; !found {
			return errors.New("payments channel-open event is required")
		}
	}
	return nil
}
