package types

import (
	"errors"
	"fmt"
	"strings"
)

func RefreshAsyncExecutionQueues(state PaymentsState, currentHeight uint64) (PaymentsState, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, errors.New("payments async queue refresh height must be positive")
	}
	next := state.Clone()
	for _, channel := range state.Channels {
		channel = channel.Normalize()
		finalizeHeight, ok := PendingFinalizationHeightForChannel(channel)
		if !ok {
			continue
		}
		jobID := asyncFinalizationJobID(channel.ChannelID, finalizeHeight)
		if _, found := asyncFinalizationJobByID(next.AsyncFinalizationQueue, jobID); found {
			continue
		}
		next.AsyncFinalizationQueue = append(next.AsyncFinalizationQueue, AsyncFinalizationJob{
			JobID:          jobID,
			ChannelID:      channel.ChannelID,
			FinalizeHeight: finalizeHeight,
			EnqueuedHeight: currentHeight,
		}.Normalize())
	}
	sortAsyncFinalizationJobs(next.AsyncFinalizationQueue)
	return next, next.Validate()
}

func EnqueueExpiredPromise(state PaymentsState, promise ConditionalPromise, resolver string, currentHeight uint64) (PaymentsState, AsyncPromiseExpiryJob, error) {
	state = state.Export()
	if currentHeight == 0 {
		return PaymentsState{}, AsyncPromiseExpiryJob{}, errors.New("payments async promise enqueue height must be positive")
	}
	promise = promise.Normalize()
	channel, found := state.ChannelByID(promise.ChannelID)
	if !found {
		return PaymentsState{}, AsyncPromiseExpiryJob{}, errors.New("payments channel not found")
	}
	if err := promise.ValidateForChannel(channel); err != nil {
		return PaymentsState{}, AsyncPromiseExpiryJob{}, err
	}
	resolver = strings.TrimSpace(resolver)
	if resolver == "" {
		resolver = promise.Source
	}
	if !containsString(channel.Participants, resolver) {
		return PaymentsState{}, AsyncPromiseExpiryJob{}, errors.New("payments async promise resolver must be participant")
	}
	expireAfterHeight := promise.TimeoutHeight + 1
	jobID := asyncPromiseExpiryJobID(channel.ChannelID, promise.PromiseID, expireAfterHeight)
	if existing, found := asyncPromiseExpiryJobByID(state.AsyncPromiseExpiryQueue, jobID); found {
		return state, existing, nil
	}
	job := AsyncPromiseExpiryJob{
		JobID:             jobID,
		ChannelID:         channel.ChannelID,
		PromiseID:         promise.PromiseID,
		Promise:           promise,
		Resolver:          resolver,
		ExpireAfterHeight: expireAfterHeight,
		EnqueuedHeight:    currentHeight,
	}.Normalize()
	if err := job.Validate(); err != nil {
		return PaymentsState{}, AsyncPromiseExpiryJob{}, err
	}
	next := state.Clone()
	next.AsyncPromiseExpiryQueue = append(next.AsyncPromiseExpiryQueue, job)
	sortAsyncPromiseExpiryJobs(next.AsyncPromiseExpiryQueue)
	return next, job, next.Validate()
}

func ProcessAsyncExecutionQueues(state PaymentsState, currentHeight, maxFinalizations, maxPromiseExpiries uint64) (PaymentsState, AsyncExecutionResult, error) {
	if currentHeight == 0 {
		return PaymentsState{}, AsyncExecutionResult{}, errors.New("payments async process height must be positive")
	}
	next, err := RefreshAsyncExecutionQueues(state, currentHeight)
	if err != nil {
		return PaymentsState{}, AsyncExecutionResult{}, err
	}
	result := AsyncExecutionResult{}
	for _, queued := range next.AsyncFinalizationQueue {
		if maxFinalizations > 0 && result.ProcessedFinalizations >= maxFinalizations {
			break
		}
		job := queued.Normalize()
		if job.Completed || job.FinalizeHeight > currentHeight {
			continue
		}
		result.ProcessedFinalizations++
		channel, found := next.ChannelByID(job.ChannelID)
		if !found {
			next = markAsyncFinalizationFailed(next, job.JobID, currentHeight, "payments channel not found")
			result.FailedJobIDs = append(result.FailedJobIDs, job.JobID)
			continue
		}
		if channel.Status == ChannelStatusSettled {
			settlementHash := latestSettlementHashForChannel(next.Settlements, channel.ChannelID)
			if settlementHash == "" {
				settlementHash = channel.OpeningStateHash
			}
			next = markAsyncFinalizationCompleted(next, job.JobID, settlementHash, currentHeight)
			next = appendAsyncCompletion(next, job.JobID, "finalization", channel.ChannelID, channel.ChannelID, settlementHash, currentHeight, &result)
			result.CompletedJobIDs = append(result.CompletedJobIDs, job.JobID)
			continue
		}
		var settlement SettlementRecord
		next, settlement, err = FinalizeSettlement(next, job.ChannelID, currentHeight)
		if err != nil {
			next = markAsyncFinalizationFailed(next, job.JobID, currentHeight, err.Error())
			result.FailedJobIDs = append(result.FailedJobIDs, job.JobID)
			continue
		}
		next = markAsyncFinalizationCompleted(next, job.JobID, settlement.SettlementHash, currentHeight)
		next = appendAsyncCompletion(next, job.JobID, "finalization", settlement.ChannelID, settlement.ChannelID, settlement.SettlementHash, currentHeight, &result)
		result.CompletedJobIDs = append(result.CompletedJobIDs, job.JobID)
	}
	for _, queued := range next.AsyncPromiseExpiryQueue {
		if maxPromiseExpiries > 0 && result.ProcessedPromiseExpiries >= maxPromiseExpiries {
			break
		}
		job := queued.Normalize()
		if job.Completed || job.ExpireAfterHeight > currentHeight {
			continue
		}
		result.ProcessedPromiseExpiries++
		channel, found := next.ChannelByID(job.ChannelID)
		if !found {
			next = markAsyncPromiseExpiryFailed(next, job.JobID, currentHeight, "payments channel not found")
			result.FailedJobIDs = append(result.FailedJobIDs, job.JobID)
			continue
		}
		if promiseWasSettled(channel, job.PromiseID, next.ConditionClaims) {
			resultHash := HashParts("async-promise-already-settled", job.ChannelID, job.PromiseID)
			next = markAsyncPromiseExpiryCompleted(next, job.JobID, resultHash, currentHeight)
			next = appendAsyncCompletion(next, job.JobID, "promise-expiry", job.ChannelID, job.PromiseID, resultHash, currentHeight, &result)
			result.CompletedJobIDs = append(result.CompletedJobIDs, job.JobID)
			continue
		}
		var resolutions []ConditionResolution
		next, resolutions, _, err = ExpireConditionalPromises(next, PromiseExpiryRequest{
			ChannelID:     job.ChannelID,
			Promises:      []ConditionalPromise{job.Promise},
			Resolver:      job.Resolver,
			CurrentHeight: currentHeight,
		})
		if err != nil {
			next = markAsyncPromiseExpiryFailed(next, job.JobID, currentHeight, err.Error())
			result.FailedJobIDs = append(result.FailedJobIDs, job.JobID)
			continue
		}
		resultHash := HashParts("async-promise-expiry", job.ChannelID, job.PromiseID, resolutions[0].EvidenceHash)
		next = markAsyncPromiseExpiryCompleted(next, job.JobID, resultHash, currentHeight)
		next = appendAsyncCompletion(next, job.JobID, "promise-expiry", job.ChannelID, job.PromiseID, resultHash, currentHeight, &result)
		result.CompletedJobIDs = append(result.CompletedJobIDs, job.JobID)
	}
	sortStrings(result.CompletedJobIDs)
	sortStrings(result.FailedJobIDs)
	sortStrings(result.EmittedCompletionIDs)
	return next, result, next.Validate()
}

func asyncFinalizationJobID(channelID string, finalizeHeight uint64) string {
	return HashParts("async-finalization-job", normalizeHash(channelID), fmt.Sprintf("%020d", finalizeHeight))
}

func asyncPromiseExpiryJobID(channelID, promiseID string, expireAfterHeight uint64) string {
	return HashParts("async-promise-expiry-job", normalizeHash(channelID), normalizeHash(promiseID), fmt.Sprintf("%020d", expireAfterHeight))
}

func asyncFinalizationJobByID(jobs []AsyncFinalizationJob, jobID string) (AsyncFinalizationJob, bool) {
	jobID = normalizeHash(jobID)
	for _, job := range jobs {
		job = job.Normalize()
		if job.JobID == jobID {
			return job, true
		}
	}
	return AsyncFinalizationJob{}, false
}

func asyncPromiseExpiryJobByID(jobs []AsyncPromiseExpiryJob, jobID string) (AsyncPromiseExpiryJob, bool) {
	jobID = normalizeHash(jobID)
	for _, job := range jobs {
		job = job.Normalize()
		if job.JobID == jobID {
			return job, true
		}
	}
	return AsyncPromiseExpiryJob{}, false
}

func markAsyncFinalizationCompleted(state PaymentsState, jobID, settlementHash string, height uint64) PaymentsState {
	jobID = normalizeHash(jobID)
	for i := range state.AsyncFinalizationQueue {
		if state.AsyncFinalizationQueue[i].Normalize().JobID == jobID {
			state.AsyncFinalizationQueue[i].Completed = true
			state.AsyncFinalizationQueue[i].CompletedHeight = height
			state.AsyncFinalizationQueue[i].SettlementHash = normalizeHash(settlementHash)
			state.AsyncFinalizationQueue[i].LastRunHeight = height
			state.AsyncFinalizationQueue[i].LastError = ""
			state.AsyncFinalizationQueue[i].Attempts++
			break
		}
	}
	sortAsyncFinalizationJobs(state.AsyncFinalizationQueue)
	return state
}

func markAsyncFinalizationFailed(state PaymentsState, jobID string, height uint64, message string) PaymentsState {
	jobID = normalizeHash(jobID)
	for i := range state.AsyncFinalizationQueue {
		if state.AsyncFinalizationQueue[i].Normalize().JobID == jobID {
			state.AsyncFinalizationQueue[i].LastRunHeight = height
			state.AsyncFinalizationQueue[i].LastError = strings.TrimSpace(message)
			state.AsyncFinalizationQueue[i].Attempts++
			break
		}
	}
	sortAsyncFinalizationJobs(state.AsyncFinalizationQueue)
	return state
}

func markAsyncPromiseExpiryCompleted(state PaymentsState, jobID, resolutionHash string, height uint64) PaymentsState {
	jobID = normalizeHash(jobID)
	for i := range state.AsyncPromiseExpiryQueue {
		if state.AsyncPromiseExpiryQueue[i].Normalize().JobID == jobID {
			state.AsyncPromiseExpiryQueue[i].Completed = true
			state.AsyncPromiseExpiryQueue[i].CompletedHeight = height
			state.AsyncPromiseExpiryQueue[i].ResolutionHash = normalizeHash(resolutionHash)
			state.AsyncPromiseExpiryQueue[i].LastRunHeight = height
			state.AsyncPromiseExpiryQueue[i].LastError = ""
			state.AsyncPromiseExpiryQueue[i].Attempts++
			break
		}
	}
	sortAsyncPromiseExpiryJobs(state.AsyncPromiseExpiryQueue)
	return state
}

func markAsyncPromiseExpiryFailed(state PaymentsState, jobID string, height uint64, message string) PaymentsState {
	jobID = normalizeHash(jobID)
	for i := range state.AsyncPromiseExpiryQueue {
		if state.AsyncPromiseExpiryQueue[i].Normalize().JobID == jobID {
			state.AsyncPromiseExpiryQueue[i].LastRunHeight = height
			state.AsyncPromiseExpiryQueue[i].LastError = strings.TrimSpace(message)
			state.AsyncPromiseExpiryQueue[i].Attempts++
			break
		}
	}
	sortAsyncPromiseExpiryJobs(state.AsyncPromiseExpiryQueue)
	return state
}

func appendAsyncCompletion(state PaymentsState, jobID, jobType, channelID, objectID, resultHash string, height uint64, result *AsyncExecutionResult) PaymentsState {
	jobID = normalizeHash(jobID)
	resultHash = normalizeHash(resultHash)
	for _, completion := range state.AsyncCompletions {
		completion = completion.Normalize()
		if completion.JobID == jobID && completion.ResultHash == resultHash {
			return state
		}
	}
	completion := AsyncSettlementCompletion{
		CompletionID: HashParts("async-settlement-completion", jobID, resultHash, fmt.Sprintf("%020d", height)),
		JobID:        jobID,
		JobType:      jobType,
		ChannelID:    channelID,
		ObjectID:     objectID,
		ResultHash:   resultHash,
		Height:       height,
	}.Normalize()
	state.AsyncCompletions = append(state.AsyncCompletions, completion)
	state.Events = append(state.Events, AsyncSettlementCompletionEvent(completion))
	sortAsyncSettlementCompletions(state.AsyncCompletions)
	if result != nil {
		result.EmittedCompletionIDs = append(result.EmittedCompletionIDs, completion.CompletionID)
	}
	return state
}
