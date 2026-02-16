package handlers

import (
	"backend/models"
	"backend/repository"
	"backend/storage"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gorm.io/gorm"
)

// GormJobManager handles background job processing with GORM
type GormJobManager struct {
	db *gorm.DB
	mu sync.RWMutex

	// Global job management (inspired by reference code)
	jobCancelMap map[int]context.CancelFunc
	jobWG        sync.WaitGroup
	jobMutex     sync.RWMutex

	// Job state tracking to prevent restarts
	terminatedJobs  map[int]bool
	terminatedMutex sync.RWMutex

	// Global termination flag to prevent database updates
	globalTerminationFlag  bool
	globalTerminationMutex sync.RWMutex

	// Global job creation blocker to prevent new jobs after termination
	jobCreationBlocked bool
	jobCreationMutex   sync.RWMutex

	// Graceful shutdown support
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
	shutdownOnce   sync.Once
	isShuttingDown bool

	// Unified job state management
	jobStates  map[int]JobState
	stateMutex sync.RWMutex
}

// JobState represents the state of a job
type JobState struct {
	JobID       int
	Status      string // "pending", "running", "cancelled", "completed", "failed"
	CreatedAt   time.Time
	StartedAt   *time.Time
	CancelledAt *time.Time
	CompletedAt *time.Time
	CancelFunc  context.CancelFunc
	Context     context.Context
}

// NewGormJobManager creates a new GORM-based job manager instance
func NewGormJobManager() *GormJobManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &GormJobManager{
		db:             storage.GetGormDB(),
		jobCancelMap:   make(map[int]context.CancelFunc),
		terminatedJobs: make(map[int]bool),
		jobStates:      make(map[int]JobState),
		shutdownCtx:    ctx,
		shutdownCancel: cancel,
	}
}

// registerJob registers a job as running
func (gjm *GormJobManager) registerJob(jobID int, cancelFunc context.CancelFunc) {
	// Use a single lock to ensure atomic registration across both maps
	gjm.jobMutex.Lock()
	defer gjm.jobMutex.Unlock()

	log.Printf("=== JOB LIFECYCLE: Starting registration for job %d ===", jobID)

	// Check if we're shutting down
	if gjm.isShuttingDown {
		log.Printf("JOB LIFECYCLE: Rejecting job %d - system is shutting down", jobID)
		cancelFunc() // Cancel immediately if shutting down
		return
	}

	// Check if job was terminated - but allow registration if it's a fresh job
	if gjm.IsJobTerminated(jobID) {
		log.Printf("JOB LIFECYCLE: Job %d was marked as terminated, but allowing registration for fresh start", jobID)
		// Clear the terminated flag for this job since we're starting fresh
		gjm.terminatedMutex.Lock()
		delete(gjm.terminatedJobs, jobID)
		gjm.terminatedMutex.Unlock()
		log.Printf("JOB LIFECYCLE: Cleared terminated flag for job %d", jobID)
	}

	// ATOMIC REGISTRATION: Register in both maps simultaneously
	gjm.jobCancelMap[jobID] = cancelFunc
	log.Printf("JOB LIFECYCLE: Job %d registered in jobCancelMap", jobID)

	// Clear global termination flags for new job
	gjm.ResetGlobalTerminationFlag()
	gjm.UnblockJobCreation()
	log.Printf("JOB LIFECYCLE: Cleared global termination flags for new job %d", jobID)

	// Verify registration in jobCancelMap
	if _, exists := gjm.jobCancelMap[jobID]; exists {
		log.Printf("JOB LIFECYCLE: Job %d successfully registered in jobCancelMap", jobID)
	} else {
		log.Printf("ERROR: Job %d registration failed in jobCancelMap", jobID)
	}

	// ATOMIC REGISTRATION: Update unified job state
	gjm.updateJobState(jobID, "running", cancelFunc)

	// Final verification - check both maps
	if _, exists := gjm.jobCancelMap[jobID]; exists {
		if _, stateExists := gjm.getJobState(jobID); stateExists {
			log.Printf("JOB LIFECYCLE: Job %d successfully registered in BOTH maps - jobCancelMap: ✓, jobStates: ✓", jobID)
		} else {
			log.Printf("ERROR: Job %d registered in jobCancelMap but NOT in jobStates", jobID)
		}
	} else {
		log.Printf("ERROR: Job %d registration failed in jobCancelMap", jobID)
	}

	log.Printf("=== JOB LIFECYCLE: Registration completed for job %d ===", jobID)

	// Add a small delay to ensure registration is fully propagated
	time.Sleep(10 * time.Millisecond)

	// Final verification after delay
	if _, exists := gjm.jobCancelMap[jobID]; exists {
		if _, stateExists := gjm.getJobState(jobID); stateExists {
			log.Printf("JOB LIFECYCLE: Final verification - Job %d successfully registered and ready for termination", jobID)
		} else {
			log.Printf("ERROR: Job %d registration incomplete - missing from jobStates after delay", jobID)
		}
	} else {
		log.Printf("ERROR: Job %d registration incomplete - missing from jobCancelMap after delay", jobID)
	}
}

// updateJobState updates the unified job state
func (gjm *GormJobManager) updateJobState(jobID int, status string, cancelFunc context.CancelFunc) {
	gjm.stateMutex.Lock()
	defer gjm.stateMutex.Unlock()

	now := time.Now()
	jobState := JobState{
		JobID:      jobID,
		Status:     status,
		CreatedAt:  now,
		CancelFunc: cancelFunc,
	}

	switch status {
	case "running":
		jobState.StartedAt = &now
	case "cancelled":
		jobState.CancelledAt = &now
	case "completed":
		jobState.CompletedAt = &now
	}

	gjm.jobStates[jobID] = jobState
	log.Printf("JOB LIFECYCLE: Updated job %d state to: %s in jobStates map", jobID, status)

	// Verify the update was successful
	if storedState, exists := gjm.jobStates[jobID]; exists {
		log.Printf("JOB LIFECYCLE: Verified job %d state update - Status: %s, CancelFunc: %v", jobID, storedState.Status, storedState.CancelFunc != nil)
	} else {
		log.Printf("ERROR: Job %d state update failed - not found in jobStates", jobID)
	}
}

// getJobState gets the current state of a job
func (gjm *GormJobManager) getJobState(jobID int) (JobState, bool) {
	gjm.stateMutex.RLock()
	defer gjm.stateMutex.RUnlock()

	jobState, exists := gjm.jobStates[jobID]
	return jobState, exists
}

// cancelJobState cancels a job using the unified state management
func (gjm *GormJobManager) cancelJobState(jobID int) bool {
	gjm.stateMutex.Lock()
	defer gjm.stateMutex.Unlock()

	log.Printf("JOB LIFECYCLE: Attempting to cancel job %d using unified state management", jobID)

	jobState, exists := gjm.jobStates[jobID]
	if !exists {
		log.Printf("JOB LIFECYCLE: Job %d not found in state management", jobID)
		return false
	}

	log.Printf("JOB LIFECYCLE: Found job %d in state management - Status: %s, CancelFunc: %v", jobID, jobState.Status, jobState.CancelFunc != nil)

	if jobState.Status == "cancelled" || jobState.Status == "completed" {
		log.Printf("JOB LIFECYCLE: Job %d already in final state: %s", jobID, jobState.Status)
		return false
	}

	// Cancel the context if it exists
	if jobState.CancelFunc != nil {
		log.Printf("JOB LIFECYCLE: Cancelling job %d context", jobID)
		jobState.CancelFunc()
		log.Printf("JOB LIFECYCLE: Successfully called cancel function for job %d", jobID)
	} else {
		log.Printf("JOB LIFECYCLE: Warning - Job %d has no cancel function", jobID)
	}

	// Update state to cancelled
	now := time.Now()
	jobState.Status = "cancelled"
	jobState.CancelledAt = &now
	gjm.jobStates[jobID] = jobState

	log.Printf("Job %d state updated to cancelled", jobID)
	return true
}

// verifyJobStatus provides comprehensive debugging information about a job's status
func (gjm *GormJobManager) verifyJobStatus(jobID int) {
	log.Printf("=== JOB STATUS VERIFICATION for job %d ===", jobID)

	// Check jobCancelMap
	gjm.jobMutex.RLock()
	if cancelFunc, exists := gjm.jobCancelMap[jobID]; exists {
		log.Printf("✓ Job %d found in jobCancelMap - CancelFunc: %v", jobID, cancelFunc != nil)
	} else {
		log.Printf("✗ Job %d NOT found in jobCancelMap", jobID)
	}
	gjm.jobMutex.RUnlock()

	// Check jobStates
	if state, exists := gjm.getJobState(jobID); exists {
		log.Printf("✓ Job %d found in jobStates - Status: %s, CancelFunc: %v", jobID, state.Status, state.CancelFunc != nil)
	} else {
		log.Printf("✗ Job %d NOT found in jobStates", jobID)
	}

	// Check terminatedJobs
	gjm.terminatedMutex.RLock()
	if terminated := gjm.terminatedJobs[jobID]; terminated {
		log.Printf("✓ Job %d marked as terminated", jobID)
	} else {
		log.Printf("✗ Job %d NOT marked as terminated", jobID)
	}
	gjm.terminatedMutex.RUnlock()

	// Check global termination flag
	if gjm.IsGlobalTerminationSet() {
		log.Printf("✓ Global termination flag is SET")
	} else {
		log.Printf("✗ Global termination flag is NOT set")
	}

	log.Printf("=== END JOB STATUS VERIFICATION for job %d ===", jobID)
}

// unregisterJob removes a job from running jobs
func (gjm *GormJobManager) unregisterJob(jobID int) {
	gjm.jobMutex.Lock()
	defer gjm.jobMutex.Unlock()

	if _, exists := gjm.jobCancelMap[jobID]; exists {
		delete(gjm.jobCancelMap, jobID)
		log.Printf("Unregistered job %d", jobID)
	}
}

// stopJob stops a specific job and its batches (inspired by reference code)
func (gjm *GormJobManager) stopJob(jobID int) bool {
	gjm.jobMutex.Lock()
	defer gjm.jobMutex.Unlock()

	if cancel, exists := gjm.jobCancelMap[jobID]; exists {
		cancel() // Stop all batches for this job
		delete(gjm.jobCancelMap, jobID)
		gjm.MarkJobAsTerminated(jobID) // Mark as terminated to prevent restarts
		gjm.SetGlobalTerminationFlag() // Set global flag to prevent database updates
		gjm.BlockJobCreation()         // Block creation of new jobs
		log.Printf("Stopping Job %d", jobID)

		// Force update job status to cancelled (bypass termination check)
		go func() {
			time.Sleep(100 * time.Millisecond)
			errorMsg := "Job cancelled by user"
			if err := gjm.ForceUpdateJobStatus(jobID, "cancelled", 0, 0, &errorMsg, nil); err != nil {
				log.Printf("Failed to force update job %d status: %v", jobID, err)
			} else {
				log.Printf("Job %d status force-updated to cancelled", jobID)
			}
		}()

		return true
	}

	log.Printf("No running job found with ID %d", jobID)
	return false
}

// StopSpecificJob stops a specific job (inspired by reference code)
func (gjm *GormJobManager) StopSpecificJob(jobID int) {
	if gjm.stopJob(jobID) {
		log.Printf("Successfully stopped job %d", jobID)
	} else {
		log.Printf("Failed to stop job %d - not found", jobID)
	}
}

// GracefulShutdown initiates graceful shutdown of all running jobs
func (gjm *GormJobManager) GracefulShutdown(timeout time.Duration) error {
	var err error
	gjm.shutdownOnce.Do(func() {
		err = gjm.gracefulShutdown(timeout)
	})
	return err
}

// gracefulShutdown performs the actual graceful shutdown
func (gjm *GormJobManager) gracefulShutdown(timeout time.Duration) error {
	log.Printf("Initiating graceful shutdown with %v timeout", timeout)

	// Mark as shutting down
	gjm.jobMutex.Lock()
	gjm.isShuttingDown = true
	jobCount := len(gjm.jobCancelMap)
	gjm.jobMutex.Unlock()

	log.Printf("Cancelling %d running jobs", jobCount)

	// Cancel all running jobs
	gjm.jobMutex.RLock()
	for jobID, cancelFunc := range gjm.jobCancelMap {
		log.Printf("Cancelling job %d", jobID)
		cancelFunc()
	}
	gjm.jobMutex.RUnlock()

	// Cancel the shutdown context
	gjm.shutdownCancel()

	// Wait for all jobs to complete with timeout
	done := make(chan struct{})
	go func() {
		gjm.jobWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("All jobs completed gracefully")
		return nil
	case <-time.After(timeout):
		log.Printf("Graceful shutdown timed out after %v", timeout)
		return fmt.Errorf("graceful shutdown timed out after %v", timeout)
	}
}

// IsShuttingDown returns true if the job manager is in shutdown mode
func (gjm *GormJobManager) IsShuttingDown() bool {
	gjm.jobMutex.RLock()
	defer gjm.jobMutex.RUnlock()
	return gjm.isShuttingDown
}

// GetRunningJobsCount returns the number of currently running jobs
func (gjm *GormJobManager) GetRunningJobsCount() int {
	gjm.jobMutex.RLock()
	defer gjm.jobMutex.RUnlock()
	return len(gjm.jobCancelMap)
}

// IsJobRunning checks if a specific job is currently running
func (gjm *GormJobManager) IsJobRunning(jobID int) bool {
	gjm.jobMutex.RLock()
	defer gjm.jobMutex.RUnlock()
	_, exists := gjm.jobCancelMap[jobID]
	return exists
}

// IsJobTerminated checks if a specific job has been terminated
func (gjm *GormJobManager) IsJobTerminated(jobID int) bool {
	gjm.terminatedMutex.RLock()
	defer gjm.terminatedMutex.RUnlock()
	return gjm.terminatedJobs[jobID]
}

// MarkJobAsTerminated marks a job as terminated to prevent restarts
func (gjm *GormJobManager) MarkJobAsTerminated(jobID int) {
	gjm.terminatedMutex.Lock()
	defer gjm.terminatedMutex.Unlock()
	gjm.terminatedJobs[jobID] = true
	log.Printf("Job %d marked as terminated", jobID)
}

// SetGlobalTerminationFlag sets the global termination flag
func (gjm *GormJobManager) SetGlobalTerminationFlag() {
	gjm.globalTerminationMutex.Lock()
	defer gjm.globalTerminationMutex.Unlock()
	gjm.globalTerminationFlag = true
	log.Println("Global termination flag set - preventing all database updates")
}

// IsGlobalTerminationSet checks if global termination is set
func (gjm *GormJobManager) IsGlobalTerminationSet() bool {
	gjm.globalTerminationMutex.RLock()
	defer gjm.globalTerminationMutex.RUnlock()
	return gjm.globalTerminationFlag
}

// ResetGlobalTerminationFlag resets the global termination flag
func (gjm *GormJobManager) ResetGlobalTerminationFlag() {
	gjm.globalTerminationMutex.Lock()
	defer gjm.globalTerminationMutex.Unlock()
	gjm.globalTerminationFlag = false
	log.Println("Global termination flag reset - database updates allowed")
}

// BlockJobCreation blocks creation of new jobs
func (gjm *GormJobManager) BlockJobCreation() {
	gjm.jobCreationMutex.Lock()
	defer gjm.jobCreationMutex.Unlock()
	gjm.jobCreationBlocked = true
	log.Println("Job creation blocked - no new jobs can be created")
}

// IsJobCreationBlocked checks if job creation is blocked
func (gjm *GormJobManager) IsJobCreationBlocked() bool {
	gjm.jobCreationMutex.RLock()
	defer gjm.jobCreationMutex.RUnlock()
	return gjm.jobCreationBlocked
}

// UnblockJobCreation allows creation of new jobs
func (gjm *GormJobManager) UnblockJobCreation() {
	gjm.jobCreationMutex.Lock()
	defer gjm.jobCreationMutex.Unlock()
	gjm.jobCreationBlocked = false
	log.Println("Job creation unblocked - new jobs can be created")
}

// StopAllJobsAndBlockNew stops all running jobs and blocks new job creation
func (gjm *GormJobManager) StopAllJobsAndBlockNew() {
	log.Println("Stopping all jobs and blocking new job creation...")

	// Block job creation first
	gjm.BlockJobCreation()

	// Stop all running jobs
	gjm.jobMutex.Lock()
	for jobID, cancel := range gjm.jobCancelMap {
		log.Printf("Stopping job %d", jobID)
		cancel()                       // Cancel the context
		gjm.MarkJobAsTerminated(jobID) // Mark as terminated
	}
	gjm.jobMutex.Unlock()

	// Set global termination flag
	gjm.SetGlobalTerminationFlag()

	log.Println("All jobs stopped and new job creation blocked")
}

// ForceUpdateJobStatus updates job status bypassing termination checks
func (gjm *GormJobManager) ForceUpdateJobStatus(jobID int, status string, progress int, processedItems int, errorMsg *string, result *string) error {
	gjm.mu.Lock()
	defer gjm.mu.Unlock()

	updates := map[string]interface{}{
		"status":          status,
		"progress":        progress,
		"processed_items": processedItems,
		"updated_at":      time.Now(),
	}

	if status == "completed" || status == "failed" {
		now := time.Now()
		updates["completed_at"] = &now
	}

	if errorMsg != nil {
		updates["error"] = errorMsg
	}

	if result != nil {
		updates["result"] = result
	}

	log.Printf("Force updating job %d status to %s", jobID, status)
	return gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Updates(updates).Error
}

// WaitForAllJobs waits for all jobs to finish (inspired by reference code)
func (gjm *GormJobManager) WaitForAllJobs() {
	gjm.jobWG.Wait()
	log.Println("All jobs and batches completed.")
}

// StartJobWithTracking starts a job with proper tracking (inspired by reference code)
func (gjm *GormJobManager) StartJobWithTracking(jobID int, numBatches int, processFunc func(context.Context, int)) {
	// Stop if already running
	if gjm.IsJobRunning(jobID) {
		log.Printf("Job %d already running", jobID)
		return
	}

	// Check if job was terminated before starting
	if gjm.IsJobTerminated(jobID) {
		log.Printf("Job %d was terminated before starting", jobID)
		return
	}

	// Check if global termination is set
	if gjm.IsGlobalTerminationSet() {
		log.Printf("Global termination set - not starting job %d", jobID)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	gjm.registerJob(jobID, cancel)

	log.Printf("Starting Job %d with %d batches", jobID, numBatches)

	// Add to WaitGroup for tracking
	gjm.jobWG.Add(1)
	go func() {
		defer func() {
			gjm.jobWG.Done()
			gjm.unregisterJob(jobID)
			log.Printf("Job %d completed and unregistered", jobID)
		}()

		// Add termination monitoring
		go func() {
			// Monitor for termination signals
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					log.Printf("Job %d context cancelled, stopping monitoring", jobID)
					return
				case <-ticker.C:
					// Check if job was terminated
					if gjm.IsJobTerminated(jobID) {
						log.Printf("Job %d termination detected, cancelling context", jobID)
						cancel() // Cancel the context to stop processing
						return
					}

					// Check if global termination is set
					if gjm.IsGlobalTerminationSet() {
						log.Printf("Job %d global termination detected, cancelling context", jobID)
						cancel() // Cancel the context to stop processing
						return
					}
				}
			}
		}()

		processFunc(ctx, jobID)
	}()
}

// ForceStopJob forcefully stops a job even if it's not in the running jobs map
func (gjm *GormJobManager) ForceStopJob(jobID int) bool {
	log.Printf("Force stopping job %d", jobID)

	// Mark job as terminated immediately
	gjm.MarkJobAsTerminated(jobID)
	log.Printf("Job %d marked as terminated", jobID)

	// Set global termination flag to prevent new database updates
	gjm.SetGlobalTerminationFlag()
	log.Printf("Global termination flag set - preventing all database updates")

	// Block new job creation
	gjm.BlockJobCreation()
	log.Printf("Job creation blocked - no new jobs can be created")

	// Get the cancel function for this job
	gjm.jobMutex.RLock()
	cancelFunc, exists := gjm.jobCancelMap[jobID]
	gjm.jobMutex.RUnlock()

	if exists && cancelFunc != nil {
		log.Printf("Found cancel function for job %d, calling it", jobID)
		cancelFunc() // Cancel the context
		log.Printf("Cancel function called for job %d", jobID)
	} else {
		log.Printf("No cancel function found for job %d", jobID)
	}

	// Force update job status to cancelled in database immediately
	forceUpdate := map[string]interface{}{
		"status":       "cancelled",
		"updated_at":   time.Now(),
		"completed_at": time.Now(),
		"error":        "Job force-cancelled by user",
		"progress":     0,
	}

	// Use a separate database connection to avoid transaction conflicts
	if err := gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Updates(forceUpdate).Error; err != nil {
		log.Printf("Failed to force update job %d status: %v", jobID, err)
	} else {
		log.Printf("Successfully force-cancelled job %d", jobID)
	}

	// Wait a short time for the job to respond to cancellation
	time.Sleep(50 * time.Millisecond)

	// Check if job is still running and force stop again
	if gjm.IsJobRunning(jobID) {
		log.Printf("Job %d still running after force stop, attempting final cleanup", jobID)

		// Try to stop the job again with a more aggressive approach
		gjm.StopSpecificJob(jobID)

		// Wait a bit more
		time.Sleep(100 * time.Millisecond)

		// Force update status again
		emergencyUpdate := map[string]interface{}{
			"status":       "cancelled",
			"updated_at":   time.Now(),
			"completed_at": time.Now(),
			"error":        "Job force-cancelled by user",
			"progress":     0,
		}

		if emergencyErr := gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Updates(emergencyUpdate).Error; emergencyErr != nil {
			log.Printf("Failed to emergency update job %d status: %v", jobID, emergencyErr)
		} else {
			log.Printf("Job %d status force-updated to cancelled", jobID)
		}
	}

	// Unregister the job
	gjm.unregisterJob(jobID)
	log.Printf("Unregistered job %d", jobID)

	return true
}

// ListRunningJobs returns a list of all running job IDs
// ListRunningJobs returns all currently running jobs
// @Summary List running jobs
// @Description Get list of currently running jobs
// @Tags Jobs
// @Accept json
// @Produce json
// @Success 200 {object} models.RunningJobsResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/jobs/running [get]
func (gjm *GormJobManager) ListRunningJobs(c *gin.Context) {
	gjm.jobMutex.RLock()
	jobIDs := make([]int, 0, len(gjm.jobCancelMap))
	for jobID := range gjm.jobCancelMap {
		jobIDs = append(jobIDs, jobID)
	}
	gjm.jobMutex.RUnlock()

	// Get job states for additional info
	jobStates := make(map[int]string)
	for _, jobID := range jobIDs {
		if state, exists := gjm.getJobState(jobID); exists {
			jobStates[jobID] = state.Status
		}
	}

	log.Printf("Currently running jobs: %v", jobIDs)
	log.Printf("Job cancel map size: %d", len(gjm.jobCancelMap))

	c.JSON(http.StatusOK, gin.H{
		"running_jobs":  jobIDs,
		"count":         len(jobIDs),
		"shutting_down": gjm.IsShuttingDown(),
		"job_states":    jobStates,
	})
}

// CreateImportJobAndGetID creates a new import job and returns the job ID using GORM
func (gjm *GormJobManager) CreateImportJobAndGetID(c *gin.Context, filePath *string) (int, error) {
	// Check if job creation is blocked
	if gjm.IsJobCreationBlocked() {
		return 0, fmt.Errorf("job creation is currently blocked due to system termination")
	}

	// Check if GORM database is initialized
	if gjm.db == nil {
		//log.Printf("Error: GORM database is not initialized")
		return 0, fmt.Errorf("database not initialized")
	}

	// Get project_id from URL parameter
	projectIDStr := c.Param("project_id")
	if projectIDStr == "" {
		return 0, fmt.Errorf("project_id is required")
	}

	projectID, err := strconv.Atoi(projectIDStr)
	if err != nil {
		return 0, fmt.Errorf("invalid project_id")
	}

	// Get job type from query parameter
	jobType := c.DefaultQuery("job_type", "element_type_import")

	// Get user info from session
	sessionID := c.GetHeader("Authorization")
	if sessionID == "" {
		return 0, fmt.Errorf("missing session ID in Authorization header")
	}

	//log.Printf("Creating import job for project ID: %d, job type: %s", projectID, jobType)

	// Test if import_jobs table exists
	var count int64
	if err := gjm.db.Model(&models.ImportJobGorm{}).Count(&count).Error; err != nil {
		//log.Printf("Error checking import_jobs table: %v", err)
		return 0, fmt.Errorf("import_jobs table not accessible: %v", err)
	}
	//log.Printf("Import_jobs table is accessible, current count: %d", count)

	// Create job record using GORM
	job := models.ImportJobGorm{
		ProjectID:      projectID,
		JobType:        jobType,
		Status:         "pending",
		Progress:       0,
		TotalItems:     0,
		ProcessedItems: 0,
		CreatedBy:      "admin", // You can get this from session
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		FilePath:       filePath,
	}

	if err := gjm.db.Create(&job).Error; err != nil {
		// log.Printf("Error creating import job: %v", err)
		// log.Printf("Job data: %+v", job)
		return 0, fmt.Errorf("failed to create job: %v", err)
	}

	//log.Printf("Successfully created import job with ID: %d", job.ID)

	// Return job ID
	return int(job.ID), nil
}

// GetJobStatus returns the current status of a job using GORM
// @Summary Get job status
// @Description Get the current status of a job
// @Tags Jobs
// @Accept json
// @Produce json
// @Param job_id path int true "Job ID"
// @Success 200 {object} models.JobStatusResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/jobs/{job_id} [get]
func (gjm *GormJobManager) GetJobStatus(c *gin.Context) {
	jobIDStr := c.Param("job_id")
	if jobIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "job_id is required"})
		return
	}

	jobID, err := strconv.Atoi(jobIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job_id"})
		return
	}

	//log.Printf("Fetching job status for job ID: %d", jobID)

	var job models.ImportJobGorm
	if err := gjm.db.First(&job, jobID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Printf("Job not found for ID: %d", jobID)
			c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
			return
		}
		log.Printf("Error fetching job status: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch job status"})
		return
	}

	//log.Printf("Successfully fetched job: ID=%d, Status=%s, Progress=%d", job.ID, job.Status, job.Progress)

	// Check if job is running in memory
	isRunning := gjm.IsJobRunning(jobID)

	// Add timing information if job is completed
	var response gin.H
	if job.Status == "completed" || job.Status == "failed" || job.Status == "completed_with_errors" {
		// Calculate elapsed time if job has completed
		elapsedTime := time.Since(job.CreatedAt)
		response = gin.H{
			"job":                  job,
			"is_running_in_memory": isRunning,
			"timing": gin.H{
				"started_at":   job.CreatedAt.Format("2006-01-02 15:04:05"),
				"completed_at": job.CompletedAt.Format("2006-01-02 15:04:05"),
				"total_time":   elapsedTime.String(),
				"elapsed_time": elapsedTime,
			},
		}
	} else {
		// For ongoing jobs, just show current elapsed time
		elapsedTime := time.Since(job.CreatedAt)
		response = gin.H{
			"job":                  job,
			"is_running_in_memory": isRunning,
			"timing": gin.H{
				"started_at":   job.CreatedAt.Format("2006-01-02 15:04:05"),
				"elapsed_time": elapsedTime.String(),
			},
		}
	}

	c.JSON(http.StatusOK, response)
}

// UpdateJobStatus updates the status of a job using GORM
func (gjm *GormJobManager) UpdateJobStatus(jobID int, status string, progress int, processedItems int, errorMsg *string, result *string) error {
	// Check if global termination is set - prevent database updates during termination
	if gjm.IsGlobalTerminationSet() {
		log.Printf("Job %d: Skipping database update due to global termination flag", jobID)
		return fmt.Errorf("job terminated - database updates blocked")
	}

	// Check if job was terminated
	if gjm.IsJobTerminated(jobID) {
		log.Printf("Job %d: Skipping database update - job was terminated", jobID)
		return fmt.Errorf("job terminated")
	}

	// Check if job creation is blocked
	if gjm.IsJobCreationBlocked() {
		log.Printf("Job %d: Skipping database update - job creation blocked", jobID)
		return fmt.Errorf("job creation blocked")
	}

	// Check if we're shutting down
	if gjm.IsShuttingDown() {
		log.Printf("Job %d: Skipping database update - system shutting down", jobID)
		return fmt.Errorf("system shutting down")
	}

	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			log.Printf("UpdateJobStatus panic recovered for job %d: %v", jobID, r)
		}
	}()

	// Use a more robust update approach with explicit field mapping
	updateData := map[string]interface{}{
		"status":          status,
		"progress":        progress,
		"processed_items": processedItems,
		"updated_at":      time.Now(),
	}

	// Only update completed_at if status is completed, failed, or cancelled
	if status == "completed" || status == "failed" || status == "cancelled" || status == "terminated" {
		updateData["completed_at"] = time.Now()
	}

	// Add error message if provided
	if errorMsg != nil {
		updateData["error"] = *errorMsg
	}

	// Add result message if provided
	if result != nil {
		updateData["result"] = *result
	}

	// Use a transaction for the update to ensure consistency
	tx := gjm.db.Begin()
	if tx.Error != nil {
		log.Printf("Failed to start transaction for job %d status update: %v", jobID, tx.Error)
		return tx.Error
	}

	defer func() {
		if r := recover(); r != nil {
			log.Printf("Transaction panic recovered for job %d: %v", jobID, r)
			tx.Rollback()
		}
	}()

	// Update the job status
	if err := tx.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Updates(updateData).Error; err != nil {
		log.Printf("Failed to update job %d status: %v", jobID, err)
		tx.Rollback()
		return err
	}

	// Commit the transaction
	if err := tx.Commit().Error; err != nil {
		log.Printf("Failed to commit transaction for job %d: %v", jobID, err)
		return err
	}

	// Log the update for debugging
	log.Printf("Job %d status updated: %s, progress: %d%%, processed: %d", jobID, status, progress, processedItems)

	return nil
}

// UpdateJobTotalItems updates the total_items field of a job using GORM
func (gjm *GormJobManager) UpdateJobTotalItems(jobID int, totalItems int) error {
	// Check if job was terminated or global termination is set
	if gjm.IsJobTerminated(jobID) || gjm.IsGlobalTerminationSet() {
		log.Printf("Skipping total items update for job %d - job terminated or global termination set", jobID)
		return nil // Skip update if terminated
	}

	gjm.mu.Lock()
	defer gjm.mu.Unlock()

	return gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Update("total_items", totalItems).Error
}

// GetJobsByProject returns all jobs for a project using GORM
// @Summary Get jobs by project
// @Description Get all jobs for a specific project
// @Tags Jobs
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {array} models.JobResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/project/{project_id}/jobs [get]
func (gjm *GormJobManager) GetJobsByProject(c *gin.Context) {
	projectIDStr := c.Param("project_id")
	if projectIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
		return
	}

	projectID, err := strconv.Atoi(projectIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}

	var jobs []models.ImportJobGorm
	if err := gjm.db.Where("project_id = ?", projectID).Order("created_at DESC").Find(&jobs).Error; err != nil {
		//log.Printf("Error fetching jobs: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch jobs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"jobs": jobs,
	})
}

// ProcessElementTypeImportJob processes the element type import in the background using GORM
func (gjm *GormJobManager) ProcessElementTypeImportJob(ctx context.Context, jobID int, projectID int, elementTypes []models.ElementType, batchSize int, concurrentBatches int) {
	// Update job status to processing
	err := gjm.UpdateJobStatus(jobID, "processing", 0, 0, nil, nil)
	if err != nil {
		//log.Printf("Error updating job status to processing: %v", err)
		return
	}

	totalElements := len(elementTypes)

	// Update total items
	err = gjm.UpdateJobTotalItems(jobID, totalElements)
	if err != nil {
		//log.Printf("Error updating job total items: %v", err)
		return
	}

	// Process elements in batches
	processedItems := 0

	for i := 0; i < totalElements; i += batchSize {
		// Check for cancellation before processing each batch
		select {
		case <-ctx.Done():
			log.Printf("Job %d cancelled during processing", jobID)
			errorMsg := "Job cancelled by user"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		default:
			// Continue processing
		}
		end := i + batchSize
		if end > totalElements {
			end = totalElements
		}

		batch := elementTypes[i:end]

		// Process batch using GORM
		batchErrors := gjm.processBatchWithGorm(ctx, batch, jobID)
		if len(batchErrors) > 0 {
			//log.Printf("Error processing batch: %v", batchErrors)
			errorMsg := fmt.Sprintf("Batch processing errors: %v", batchErrors)
			gjm.UpdateJobStatus(jobID, "failed", 0, processedItems, &errorMsg, nil)
			return
		}

		processedItems += len(batch)
		progress := (processedItems * 100) / totalElements

		// Update progress
		updateErr := gjm.UpdateJobStatus(jobID, "processing", progress, processedItems, nil, nil)
		if updateErr != nil {
			//log.Printf("Error updating job progress: %v", updateErr)
		}
	}

	// Mark job as completed
	result := fmt.Sprintf("Successfully processed %d elements", processedItems)
	gjm.UpdateJobStatus(jobID, "completed", 100, processedItems, nil, &result)
}

// ProcessElementTypeExcelImportJobFromPath processes Excel import from server file path using GORM
func (gjm *GormJobManager) ProcessElementTypeExcelImportJobFromPath(ctx context.Context, jobID int, projectID int, filePath string, batchSize int, concurrentBatches int, userName string, session models.Session) {
	// Start timing the entire process
	startTime := time.Now()
	//	log.Printf("Starting import job %d at %s", jobID, startTime.Format("2006-01-02 15:04:05"))

	// Update job status to processing
	err := gjm.UpdateJobStatus(jobID, "processing", 0, 0, nil, nil)
	if err != nil {
		//	log.Printf("Error updating job status to processing: %v", err)
		return
	}

	// Check if file exists
	if !FileExists(filePath) {
		errorMsg := fmt.Sprintf("File not found at path: %s", filePath)
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	// Open Excel file from server path
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		errorMsg := fmt.Sprintf("Unable to open Excel file: %v", err)
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}
	defer f.Close()

	// Get all sheets
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		errorMsg := "No sheets found in Excel file"
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	// Look for "Element Types" sheet
	elementTypesSheet := "Element Types"
	sheetFound := false
	for _, sheet := range sheets {
		if sheet == elementTypesSheet {
			sheetFound = true
			break
		}
	}

	if !sheetFound {
		errorMsg := fmt.Sprintf("Sheet '%s' not found in Excel file", elementTypesSheet)
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	// Read data from Excel sheet
	rows, err := f.GetRows(elementTypesSheet)
	if err != nil {
		errorMsg := fmt.Sprintf("Error reading Excel sheet: %v", err)
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	if len(rows) < 2 {
		errorMsg := "Excel file must have at least a header row and one data row"
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	// Get project counts for summary
	//drawingTypeCount, stageCount, precastCount, invBomCount := gjm.getProjectCounts(projectID)

	// Log export summary
	// log.Printf("=== ELEMENT TYPE EXPORT SUMMARY ===")
	// log.Printf("Project ID: %d", projectID)
	// log.Printf("Total Drawing Types: %d (Range: N1-P1)", drawingTypeCount)
	// log.Printf("Total BOM Products: %d (Range: Q1-DB1)", invBomCount)
	// log.Printf("Total Paths: %d (Range: Q1-P1)", precastCount)
	// log.Printf("Total Stages: %d (Range: I1-M1)", stageCount)
	// log.Printf("Base Columns: 8 (Range: A1-H1)")
	// log.Printf("=====================================")

	// Parse summary sheet ranges first
	ranges, summaryErr := gjm.parseSummarySheetRanges(f, projectID)
	var elementTypes []models.ElementType
	var parseErr error

	if summaryErr != nil {
		// Fallback to old method if summary sheet parsing fails
		log.Printf("Warning: Could not parse summary sheet, using fallback method: %v", summaryErr)
		elementTypes, parseErr = gjm.parseExcelDataToElementTypes(rows, projectID, userName)
	} else {
		// Use new parsing method with summary sheet ranges
		elementTypes, parseErr = gjm.parseExcelDataToElementTypesWithRanges(rows, ranges, projectID, userName)
	}

	if parseErr != nil {
		errorMsg := fmt.Sprintf("Error parsing Excel data: %v", parseErr)
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	totalElements := len(elementTypes)
	//log.Printf("Processing %d element types from Excel file", totalElements)

	// Update total items in job
	err = gjm.UpdateJobTotalItems(jobID, totalElements)
	if err != nil {
		//log.Printf("Error updating total items: %v", err)
	}

	// Process elements in batches
	processedItems := 0
	successCount := 0
	errorCount := 0
	var errors []string

	// Channel and goroutine for periodic progress update
	progressChan := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-progressChan:
				return
			case <-ticker.C:
				// Update progress in DB every 10 seconds
				gjm.UpdateJobStatus(jobID, "processing", (processedItems*100)/totalElements, processedItems, nil, nil)
			}
		}
	}()

	for i := 0; i < totalElements; i += batchSize {
		// Check for cancellation before processing each batch
		select {
		case <-ctx.Done():
			log.Printf("Job %d cancelled during processing", jobID)
			errorMsg := "Job cancelled by user"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			close(progressChan)
			return
		default:
			// Continue processing
		}
		end := i + batchSize
		if end > totalElements {
			end = totalElements
		}

		batch := elementTypes[i:end]

		// Time the batch processing
		batchStartTime := time.Now()

		// Process batch using GORM
		batchErrors := gjm.processBatchWithGorm(ctx, batch, jobID)
		batchTime := time.Since(batchStartTime)

		if len(batchErrors) > 0 {
			errors = append(errors, batchErrors...)
			errorCount += len(batchErrors)
		} else {
			successCount += len(batch)
		}

		processedItems += len(batch)
		progress := (processedItems * 100) / totalElements

		// Update progress
		updateErr := gjm.UpdateJobStatus(jobID, "processing", progress, processedItems, nil, nil)
		if updateErr != nil {
			log.Printf("Error updating job progress: %v", updateErr)
		}

		// Log batch progress with timing
		log.Printf("Processed batch %d/%d: %d items, %d errors, batch time: %v",
			(i/batchSize)+1, (totalElements+batchSize-1)/batchSize, len(batch), len(batchErrors), batchTime)
	}

	// Signal the progress updater to stop
	close(progressChan)

	// Determine final status and result
	var finalStatus string
	var resultMsg string
	var errorMsg *string

	if errorCount == 0 {
		finalStatus = "completed"
		resultMsg = fmt.Sprintf("Successfully processed all %d elements", successCount)
	} else if successCount == 0 {
		finalStatus = "failed"
		errorMsgStr := fmt.Sprintf("Failed to process any elements. Errors: %v", errors)
		errorMsg = &errorMsgStr
	} else {
		finalStatus = "completed_with_errors"
		errorDetails := strings.Join(errors, "\n")
		resultMsg = fmt.Sprintf("Processed %d elements successfully, %d errors occurred", successCount, errorCount)
		errorMsg = &errorDetails
	}

	// Calculate total time
	totalTime := time.Since(startTime)

	// Update final job status with timing information
	finalResultMsg := fmt.Sprintf("%s. Total time: %v", resultMsg, totalTime)
	gjm.UpdateJobStatus(jobID, finalStatus, 100, processedItems, errorMsg, &finalResultMsg)

	// Log completion with timing
	// log.Printf("Job %d completed with status: %s, processed: %d, errors: %d, total time: %v",
	// 	jobID, finalStatus, successCount, errorCount, totalTime)

	// Log detailed timing summary
	log.Printf("=== TIMING SUMMARY FOR JOB %d ===", jobID)
	log.Printf("Total time: %v", totalTime)
	log.Printf("Average time per element: %v", func() time.Duration {
		if successCount > 0 {
			return totalTime / time.Duration(successCount)
		}
		return 0
	}())
	log.Printf("Success rate: %.2f%% (%d/%d)", func() float64 {
		if totalElements > 0 {
			return float64(successCount) / float64(totalElements) * 100
		}
		return 0
	}(), successCount, totalElements)
	log.Printf("=====================================")
}

// getProjectCounts gets the counts for different sections like the CSV handler
func (gjm *GormJobManager) getProjectCounts(projectID int) (drawingTypeCount, stageCount, precastCount, invBomCount int) {
	// Count drawing types
	if err := gjm.db.Raw("SELECT COUNT(*) FROM drawing_type WHERE project_id = ?", projectID).Scan(&drawingTypeCount).Error; err != nil {
		log.Printf("Error counting drawing types: %v", err)
	}
	log.Printf("Drawing Type Count: %d", drawingTypeCount)

	// Count stages
	if err := gjm.db.Raw("SELECT COUNT(*) FROM project_stages WHERE project_id = ?", projectID).Scan(&stageCount).Error; err != nil {
		log.Printf("Error counting stages: %v", err)
	}
	log.Printf("Stage Count: %d", stageCount)

	// Count precast fields (paths)
	if err := gjm.db.Raw("SELECT COUNT(*) FROM precast WHERE project_id = ?", projectID).Scan(&precastCount).Error; err != nil {
		log.Printf("Error counting precast fields: %v", err)
	}
	log.Printf("Precast Count: %d for project ID: %d", precastCount, projectID)

	// Debug: Show what's in the precast table for this project
	if precastCount == 0 {
		log.Printf("DEBUG: No precast records found for project %d - this will skip hierarchy processing!", projectID)
		var sampleCount int
		if err := gjm.db.Raw("SELECT COUNT(*) FROM precast").Scan(&sampleCount).Error; err == nil {
			log.Printf("DEBUG: Total precast records in database: %d", sampleCount)
		}
	}

	// Count inv_bom fields
	if err := gjm.db.Raw("SELECT COUNT(*) FROM inv_bom WHERE project_id = ?", projectID).Scan(&invBomCount).Error; err != nil {
		log.Printf("Error counting inv_bom fields: %v", err)
	}
	log.Printf("Inv BOM Count: %d", invBomCount)

	return drawingTypeCount, stageCount, precastCount, invBomCount
}

// SummarySheetRanges holds the column ranges for different sections
type SummarySheetRanges struct {
	BaseColumns  RangeInfo
	DrawingTypes RangeInfo
	Hierarchy    RangeInfo
	Stages       RangeInfo
	BOMTypes     RangeInfo
}

// RangeInfo holds start and end column information
type RangeInfo struct {
	Start int
	End   int
	Count int
}

// parseSummarySheetRanges parses the summary sheet to get column ranges for different sections
// Structure is FIXED - only ranges can change (e.g., L1-R1, M1-S1, etc.)
func (gjm *GormJobManager) parseSummarySheetRanges(f *excelize.File, projectID int) (*SummarySheetRanges, error) {
	// Look for "Summary" sheet
	summarySheet := "Summary"
	sheets := f.GetSheetList()
	sheetFound := false
	for _, sheet := range sheets {
		if sheet == summarySheet {
			sheetFound = true
			break
		}
	}

	if !sheetFound {
		return nil, fmt.Errorf("summary sheet not found in Excel file")
	}

	// Read data from Summary sheet
	rows, err := f.GetRows(summarySheet)
	if err != nil {
		return nil, fmt.Errorf("error reading summary sheet: %v", err)
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("summary sheet must have at least a header row and one data row")
	}

	ranges := &SummarySheetRanges{}

	// Parse the summary data
	for _, row := range rows {
		if len(row) < 3 {
			continue
		}

		label := strings.TrimSpace(row[0])
		value := strings.TrimSpace(row[1])
		rangeStr := ""
		if len(row) > 2 {
			rangeStr = strings.TrimSpace(row[2])
			// Remove "Range:" prefix if present
			if strings.HasPrefix(rangeStr, "Range:") {
				rangeStr = strings.TrimSpace(strings.TrimPrefix(rangeStr, "Range:"))
			}
		}

		// Parse the value
		count, err := strconv.Atoi(value)
		if err != nil {
			continue
		}

		// Parse the range if provided
		var start, end int
		if rangeStr != "" {
			log.Printf("Parsing range '%s' for %s", rangeStr, label)
			start, end, err = parseRange(rangeStr)
			if err != nil {
				log.Printf("Error parsing range '%s' for %s: %v", rangeStr, label, err)
				continue
			}
			log.Printf("Successfully parsed range '%s' -> start: %d, end: %d", rangeStr, start, end)
		}

		// Map the data based on label - structure is fixed, only ranges change
		switch label {
		case "Base Columns":
			ranges.BaseColumns = RangeInfo{Start: start, End: end, Count: count}
		case "Total Drawing Types":
			ranges.DrawingTypes = RangeInfo{Start: start, End: end, Count: count}
		case "Total Hierarchy":
			ranges.Hierarchy = RangeInfo{Start: start, End: end, Count: count}
		case "Total Stages":
			ranges.Stages = RangeInfo{Start: start, End: end, Count: count}
		case "Total BOM Types":
			ranges.BOMTypes = RangeInfo{Start: start, End: end, Count: count}
		}
	}

	log.Printf("Parsed Summary Sheet Ranges:")
	log.Printf("  Base Columns: %d-%d (count: %d)", ranges.BaseColumns.Start, ranges.BaseColumns.End, ranges.BaseColumns.Count)
	log.Printf("  Drawing Types: %d-%d (count: %d)", ranges.DrawingTypes.Start, ranges.DrawingTypes.End, ranges.DrawingTypes.Count)
	log.Printf("  Hierarchy: %d-%d (count: %d)", ranges.Hierarchy.Start, ranges.Hierarchy.End, ranges.Hierarchy.Count)
	log.Printf("  Stages: %d-%d (count: %d)", ranges.Stages.Start, ranges.Stages.End, ranges.Stages.Count)
	log.Printf("  BOM Types: %d-%d (count: %d)", ranges.BOMTypes.Start, ranges.BOMTypes.End, ranges.BOMTypes.Count)

	// Fallback: If hierarchy count is 0 but we have precast data, use database counts
	if ranges.Hierarchy.Count == 0 {
		log.Printf("No hierarchy data found in Summary sheet, checking database...")
		_, _, precastCount, _ := gjm.getProjectCounts(projectID)
		if precastCount > 0 {
			log.Printf("Found %d precast entries in database, using as hierarchy count", precastCount)
			// Estimate hierarchy range based on typical Excel structure
			// This is a fallback - the actual range should be determined from the Excel file structure
			ranges.Hierarchy = RangeInfo{
				Start: ranges.DrawingTypes.End + 1,
				End:   ranges.DrawingTypes.End + precastCount,
				Count: precastCount,
			}
			log.Printf("Fallback Hierarchy: %d-%d (count: %d)", ranges.Hierarchy.Start, ranges.Hierarchy.End, ranges.Hierarchy.Count)
		}
	}

	// Additional fallback: Try to detect hierarchy columns by analyzing the Excel structure
	if ranges.Hierarchy.Count == 0 {
		log.Printf("Attempting to detect hierarchy columns from Excel structure...")
		// This would require analyzing the actual Excel file structure
		// For now, we'll rely on the database fallback above
	}

	return ranges, nil
}

// parseRange parses Excel range notation like "A1-G1" into start and end column indices
func parseRange(rangeStr string) (start, end int, err error) {
	// Remove any whitespace
	rangeStr = strings.TrimSpace(rangeStr)

	// Remove "Range:" prefix if present
	if strings.HasPrefix(rangeStr, "Range:") {
		rangeStr = strings.TrimSpace(strings.TrimPrefix(rangeStr, "Range:"))
	}

	// Split by dash
	parts := strings.Split(rangeStr, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format: %s", rangeStr)
	}

	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])

	// Parse start column
	start, err = parseColumnIndex(startStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid start column: %s", startStr)
	}

	// Parse end column
	end, err = parseColumnIndex(endStr)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid end column: %s", endStr)
	}

	return start, end, nil
}

// parseColumnIndex converts Excel column notation (A, B, C, ..., AA, AB, etc.) to 0-based index
func parseColumnIndex(colStr string) (int, error) {
	// Remove row number if present (e.g., "A1" -> "A")
	colStr = strings.TrimRight(colStr, "0123456789")

	if colStr == "" {
		return 0, fmt.Errorf("empty column string")
	}

	colStr = strings.ToUpper(colStr)
	result := 0

	for _, char := range colStr {
		if char < 'A' || char > 'Z' {
			return 0, fmt.Errorf("invalid character in column: %c", char)
		}
		result = result*26 + int(char-'A') + 1
	}

	return result - 1, nil // Convert to 0-based index
}

// parseExcelDataToElementTypesWithRanges parses Excel rows into ElementType models using summary sheet ranges
// Structure is FIXED: Row 1=Main Headers, Row 2=Sub Headers, Row 3=Sample Data, Row 4+=Data
// Only ranges can change (e.g., L1-R1, M1-S1, etc.)
func (gjm *GormJobManager) parseExcelDataToElementTypesWithRanges(rows [][]string, ranges *SummarySheetRanges, projectID int, userName string) ([]models.ElementType, error) {
	var elementTypes []models.ElementType

	if len(rows) < 4 {
		return nil, fmt.Errorf("excel file must have at least 3 header rows (main header, sub header, sample data) and one data row")
	}

	// Get headers from first and second rows for new format
	mainHeaders := rows[0] // Main headers (merged headings)
	subHeaders := rows[1]  // Sub headers (individual column names)
	sampleData := rows[2]  // Sample data row (non-editable)

	if len(mainHeaders) < 7 || len(subHeaders) < 7 { // Minimum required columns (0-6 for element type data)
		return nil, fmt.Errorf("excel file must have at least 7 columns (Element Type, Element Type Name, Thickness, Length, Height, Weight, Element Type Version)")
	}

	// Create combined headers by merging main headers with sub-headers using underscore
	combinedHeaders := make([]string, len(subHeaders))
	for i := range subHeaders {
		if i < len(mainHeaders) && mainHeaders[i] != "" && subHeaders[i] != "" {
			// Combine main header and sub-header with underscore
			combinedHeaders[i] = mainHeaders[i] + "_" + subHeaders[i]
		} else if subHeaders[i] != "" {
			// Use only sub-header if main header is empty
			combinedHeaders[i] = subHeaders[i]
		} else if i < len(mainHeaders) && mainHeaders[i] != "" {
			// Use only main header if sub-header is empty
			combinedHeaders[i] = mainHeaders[i]
		} else {
			// Both are empty, use column index
			combinedHeaders[i] = fmt.Sprintf("Column_%d", i+1)
		}
	}

	// Use ranges from summary sheet
	baseColumnsEnd := ranges.BaseColumns.End
	drawingStart := ranges.DrawingTypes.Start
	drawingEnd := ranges.DrawingTypes.End
	hierarchyStart := ranges.Hierarchy.Start
	hierarchyEnd := ranges.Hierarchy.End
	stageStart := ranges.Stages.Start
	stageEnd := ranges.Stages.End
	bomStart := ranges.BOMTypes.Start
	bomEnd := ranges.BOMTypes.End

	log.Printf("Using Summary Sheet Ranges:")
	log.Printf("  Base Columns: 0-%d", baseColumnsEnd)
	log.Printf("  Drawing Types: %d-%d", drawingStart, drawingEnd)
	log.Printf("  Hierarchy: %d-%d", hierarchyStart, hierarchyEnd)
	log.Printf("  Stages: %d-%d", stageStart, stageEnd)
	log.Printf("  BOM Types: %d-%d", bomStart, bomEnd)

	// Log sample data for debugging
	log.Printf("Sample data row (non-editable): %v", sampleData)

	// Process data rows (skip 3 header rows: main header, sub header, sample data)
	for i := 3; i < len(rows); i++ {
		row := rows[i]
		if len(row) < baseColumnsEnd { // Minimum required columns
			continue
		}

		// Parse basic element type data (base columns) per template image
		elementType := models.ElementType{
			ElementType:     row[0], // Element Type
			ElementTypeName: row[1], // Element Type Name
			Height:          parseFloat(row[2]),
			Length:          parseFloat(row[3]),
			Thickness:       parseFloat(row[4]),
			Mass:            parseFloat(row[5]),
			Volume: func() float64 {
				if len(row) > 6 {
					return parseFloat(row[6])
				}
				return 0
			}(),
			Area: func() float64 {
				if len(row) > 7 {
					return parseFloat(row[7])
				}
				return 0
			}(),
			Width: func() float64 {
				if len(row) > 8 {
					return parseFloat(row[8])
				}
				return 0
			}(),
			ElementTypeVersion: func() string {
				if len(row) > 9 {
					return strings.TrimSpace(row[9])
				}
				return ""
			}(),
			CreatedBy:         userName,
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
			ProjectID:         projectID,
			TotalCountElement: 1,
		}

		// Calculate density if possible
		if elementType.Volume > 0 {
			elementType.Density = elementType.Mass / elementType.Volume
		}

		// Process stages - use only sub headers (as per requirement)
		var stages []models.Stages
		if ranges.Stages.Count > 0 {
			log.Printf("Processing stages from columns %d to %d (count: %d)", stageStart, stageEnd, ranges.Stages.Count)
			var allStageIDs []int
			for j := stageStart; j <= stageEnd && j < len(row); j++ {
				cellValue := strings.TrimSpace(strings.ToLower(row[j]))
				if cellValue == "yes" || cellValue == "1" {
					// Use only sub-header for stages
					stageName := subHeaders[j]
					if stageName != "" {
						log.Printf("Stage column %d: %s = %s", j, stageName, cellValue)
						var stageID int
						if err := gjm.db.Raw("SELECT id FROM project_stages WHERE name = ? AND project_id = ?", stageName, projectID).Scan(&stageID).Error; err == nil && stageID > 0 {
							allStageIDs = append(allStageIDs, stageID)
						}
					}
				}
			}
			if len(allStageIDs) > 0 {
				stages = append(stages, models.Stages{
					StagePath: allStageIDs,
				})
			}
		}
		elementType.Stage = stages

		// Process drawings - use combined headers
		var drawings []models.Drawings
		if ranges.DrawingTypes.Count > 0 {
			log.Printf("Processing drawings from columns %d to %d (count: %d)", drawingStart, drawingEnd, ranges.DrawingTypes.Count)
			for j := drawingStart; j <= drawingEnd && j < len(row); j++ {
				file := strings.TrimSpace(row[j])
				if file != "" {
					drawingTypeName := combinedHeaders[j]
					log.Printf("Drawing column %d: %s = %s", j, drawingTypeName, file)
					var drawingTypeID int
					if err := gjm.db.Raw("SELECT drawing_type_id FROM drawing_type WHERE drawing_type_name = ? AND project_id = ?", drawingTypeName, projectID).Scan(&drawingTypeID).Error; err == nil && drawingTypeID > 0 {
						drawings = append(drawings, models.Drawings{
							DrawingsId:      repository.GenerateRandomNumber(),
							CreatedAt:       time.Now(),
							UpdateAt:        time.Now(),
							ProjectId:       projectID,
							CurrentVersion:  "VR-1",
							File:            file,
							DrawingTypeId:   drawingTypeID,
							DrawingTypeName: drawingTypeName,
							ElementTypeID:   0, // To be set later
						})
					}
				}
			}
		}
		elementType.Drawings = drawings

		// Process hierarchy - merge header and sub-header with dot notation for ltree format (e.g., Tower_G6.Floor_1)
		hierarchyQuantity := []models.HierarchyQuantity{}
		if ranges.Hierarchy.Count > 0 {
			log.Printf("Processing hierarchy from columns %d to %d (count: %d)", hierarchyStart, hierarchyEnd, ranges.Hierarchy.Count)

			// Debug: Print hierarchy headers
			log.Printf("=== DEBUG: Hierarchy Headers (New Function) ===")
			for j := hierarchyStart; j <= hierarchyEnd && j < len(row); j++ {
				mainHeader := ""
				subHeader := ""
				if j < len(mainHeaders) {
					mainHeader = mainHeaders[j]
				}
				if j < len(subHeaders) {
					subHeader = subHeaders[j]
				}
				log.Printf("Column %d: mainHeader='%s', subHeader='%s'", j, mainHeader, subHeader)
			}
			log.Printf("=== END DEBUG ===")

			for j := hierarchyStart; j <= hierarchyEnd && j < len(row); j++ {
				quantity, err := strconv.Atoi(row[j])
				if err != nil || quantity == 0 {
					continue // Skip invalid or zero values
				}

				// Find the main header for this hierarchy section (look backwards from current position)
				mainHeader := ""
				for k := j; k >= hierarchyStart; k-- {
					if k < len(mainHeaders) && mainHeaders[k] != "" {
						mainHeader = mainHeaders[k]
						break
					}
				}

				// Get the sub-header for current column
				subHeader := ""
				if j < len(subHeaders) {
					subHeader = subHeaders[j]
				}

				// Create hierarchy name with proper header/sub-header handling
				hierarchyName := createHierarchyName(mainHeader, subHeader)
				if hierarchyName == "" {
					continue // Skip if no valid name
				}

				log.Printf("DEBUG: Column %d - Header='%s', SubHeader='%s', Created hierarchy name: '%s'", j, mainHeader, subHeader, hierarchyName)

				// Try multiple variations to find the hierarchy ID
				var hierarchyID int
				var found bool
				var actualNamingConvention string

				// Sanitize hierarchy name for ltree (replace hyphens with underscores)
				sanitizedHierarchyName := sanitizeHierarchyNameForLtree(hierarchyName)

				// Try the exact sanitized name first
				if err := gjm.db.Raw("SELECT id FROM precast WHERE path = ? AND project_id = ?", sanitizedHierarchyName, projectID).Scan(&hierarchyID).Error; err == nil && hierarchyID > 0 {
					found = true
					log.Printf("Found hierarchy ID %d for exact naming convention '%s'", hierarchyID, sanitizedHierarchyName)
				} else {
					// Try with different variations (all sanitized)
					variations := generateHierarchyVariations(hierarchyName)
					log.Printf("DEBUG: Trying %d variations for hierarchy name '%s'", len(variations), hierarchyName)
					for i, variation := range variations {
						// Sanitize each variation for ltree compatibility
						sanitizedVariation := sanitizeHierarchyNameForLtree(variation)
						log.Printf("DEBUG: Trying variation %d: '%s' (sanitized: '%s')", i+1, variation, sanitizedVariation)
						if err := gjm.db.Raw("SELECT id FROM precast WHERE path = ? AND project_id = ?", sanitizedVariation, projectID).Scan(&hierarchyID).Error; err == nil && hierarchyID > 0 {
							found = true
							log.Printf("Found hierarchy ID %d for variation '%s' (original: '%s')", hierarchyID, sanitizedVariation, hierarchyName)
							break
						} else if err != nil {
							// Log ltree syntax errors gracefully without breaking the flow
							log.Printf("DEBUG: Query error for variation '%s': %v (this is expected if path doesn't exist)", sanitizedVariation, err)
						}
					}
				}

				if !found {
					log.Printf("Warning: Could not find hierarchy ID for naming convention '%s' in precast table", hierarchyName)
					continue // Skip this hierarchy entry if not found in precast table
				}

				// Fetch the actual naming_convention from the precast table
				if err := gjm.db.Raw("SELECT naming_convention FROM precast WHERE id = ? AND project_id = ?", hierarchyID, projectID).Scan(&actualNamingConvention).Error; err != nil {
					log.Printf("Warning: Could not fetch naming_convention for hierarchy ID %d, using original: %v", hierarchyID, err)
					actualNamingConvention = hierarchyName // Fallback to original if fetch fails
				}

				log.Printf("Hierarchy column %d: %s = %d (ID: %d, actual naming_convention: '%s')", j, hierarchyName, quantity, hierarchyID, actualNamingConvention)
				hierarchyQuantity = append(hierarchyQuantity, models.HierarchyQuantity{
					HierarchyId:      hierarchyID,
					Quantity:         quantity,
					NamingConvention: actualNamingConvention, // Use the actual naming_convention from database
				})
			}
		}
		elementType.HierarchyQ = hierarchyQuantity
		log.Printf("DEBUG: Final hierarchy quantity count: %d", len(hierarchyQuantity))
		if len(hierarchyQuantity) == 0 {
			log.Printf("DEBUG: No hierarchy data found - this might be the issue!")
		}
		for i, hq := range hierarchyQuantity {
			log.Printf("DEBUG: HierarchyQ[%d]: ID=%d, Quantity=%d, NamingConvention='%s'", i, hq.HierarchyId, hq.Quantity, hq.NamingConvention)
		}
		elementType.TotalCountElement = len(hierarchyQuantity)

		// Process BOM products - merge header and sub-header with underscore (e.g., Steel_6MM)
		var products []models.Product
		if ranges.BOMTypes.Count > 0 {
			log.Printf("Processing BOM from columns %d to %d (count: %d)", bomStart, bomEnd, ranges.BOMTypes.Count)
			for j := bomStart; j <= bomEnd && j < len(row); j++ {
				quantity, err := strconv.ParseFloat(row[j], 64)
				if err != nil || quantity == 0 {
					continue
				}

				// Merge header and sub-header for BOM - handle main headers that span multiple columns
				var productName string
				var mainHeader string

				// Look backwards to find the main header that spans this column
				for k := j; k >= 0; k-- {
					if k < len(mainHeaders) && mainHeaders[k] != "" {
						mainHeader = mainHeaders[k]
						break
					}
				}

				// Get sub-header from current column
				var subHeader string
				if j < len(subHeaders) {
					subHeader = subHeaders[j]
				}

				// Merge main header and sub-header
				if mainHeader != "" && subHeader != "" {
					productName = mainHeader + "_" + subHeader
				} else if subHeader != "" {
					productName = subHeader
				} else if mainHeader != "" {
					productName = mainHeader
				} else {
					continue // Skip if no valid name
				}

				log.Printf("BOM column %d: %s = %.2f", j, productName, quantity)
				var productID int
				if err := gjm.db.Raw("SELECT id FROM inv_bom WHERE name_id = ? AND project_id = ?", productName, projectID).Scan(&productID).Error; err == nil && productID > 0 {
					products = append(products, models.Product{
						ProductID:   productID,
						ProductName: productName,
						Quantity:    quantity,
					})
				}
			}
		}
		elementType.Products = products

		elementTypes = append(elementTypes, elementType)
	}

	return elementTypes, nil
}

// parseExcelDataToElementTypes parses Excel rows into ElementType models (fallback method)
func (gjm *GormJobManager) parseExcelDataToElementTypes(rows [][]string, projectID int, userName string) ([]models.ElementType, error) {
	var elementTypes []models.ElementType

	if len(rows) < 4 {
		return nil, fmt.Errorf("excel file must have at least 3 header rows (main header, sub header, sample data) and one data row")
	}

	// Get headers from first and second rows for new format
	mainHeaders := rows[0] // Main headers (merged headings)
	subHeaders := rows[1]  // Sub headers (individual column names)
	_ = rows[2]            // Sample data row (non-editable) - skip for fallback

	if len(mainHeaders) < 7 || len(subHeaders) < 7 { // Minimum required columns (0-6 for element type data)
		return nil, fmt.Errorf("excel file must have at least 7 columns (Element Type, Element Type Name, Thickness, Length, Height, Weight, Element Type Version)")
	}

	// Create combined headers by merging main headers with sub-headers using underscore
	// Handle main headers that span multiple columns (look backwards logic)
	combinedHeaders := make([]string, len(subHeaders))
	for i := range subHeaders {
		var mainHeader string
		var subHeader string

		// Look backwards to find the main header that spans this column
		for k := i; k >= 0; k-- {
			if k < len(mainHeaders) && mainHeaders[k] != "" {
				mainHeader = mainHeaders[k]
				break
			}
		}

		// Get sub-header from current column
		if i < len(subHeaders) {
			subHeader = subHeaders[i]
		}

		// Merge main header and sub-header
		if mainHeader != "" && subHeader != "" {
			combinedHeaders[i] = mainHeader + "_" + subHeader
		} else if subHeader != "" {
			combinedHeaders[i] = subHeader
		} else if mainHeader != "" {
			combinedHeaders[i] = mainHeader
		} else {
			combinedHeaders[i] = fmt.Sprintf("Column_%d", i+1)
		}
	}

	// Get counts to determine header sections
	drawingTypeCount, stageCount, precastCount, invBomCount := gjm.getProjectCounts(projectID)

	// Define column ranges based on counts
	elementTypeEnd := 10 // Columns 0-9: Element Type data as per template image
	stageStart := elementTypeEnd
	stageEnd := stageStart + stageCount
	drawingStart := stageEnd
	drawingEnd := drawingStart + drawingTypeCount
	pathStart := drawingEnd
	pathEnd := pathStart + precastCount
	bomStart := pathEnd

	log.Printf("Fallback Header structure: Element Type (0-%d), Stages (%d-%d), Drawings (%d-%d), Paths (%d-%d), BOM (%d+)",
		elementTypeEnd-1, stageStart, stageEnd-1, drawingStart, drawingEnd-1, pathStart, pathEnd-1, bomStart)

	// Locate version column by header name (robust to column order)
	versionColIdx := -1
	for idx, h := range combinedHeaders {
		lh := strings.ToLower(strings.TrimSpace(h))
		if lh == "element type version" || lh == "version" {
			versionColIdx = idx
			break
		}
	}

	// Process data rows (skip 3 header rows: main header, sub header, sample data)
	for i := 3; i < len(rows); i++ {
		row := rows[i]
		if len(row) < elementTypeEnd { // Minimum required columns
			continue
		}

		// Parse basic element type data (columns 0-9) per provided template image
		elementType := models.ElementType{
			ElementType:     row[0],             // Element Type
			ElementTypeName: row[1],             // Element Type Name
			Height:          parseFloat(row[2]), // Height
			Length:          parseFloat(row[3]), // Length
			Thickness:       parseFloat(row[4]), // Thickness
			Mass:            parseFloat(row[5]), // Mass
			Volume:          parseFloat(row[6]), // Volume
			Area:            parseFloat(row[7]), // Area
			Width:           parseFloat(row[8]), // Width
			ElementTypeVersion: func() string {
				if versionColIdx >= 0 && versionColIdx < len(row) {
					return strings.TrimSpace(row[versionColIdx])
				}
				return row[9] // fallback to current mapping
			}(),
			CreatedBy:         userName,
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
			ProjectID:         projectID,
			TotalCountElement: 1,
		}

		// Calculate density if mass and volume are provided (> 0)
		if elementType.Volume > 0 {
			elementType.Density = elementType.Mass / elementType.Volume
		}

		// Process stages (columns 10 onwards based on stageCount) - use combined headers
		var stages []models.Stages
		if stageCount > 0 {
			var allStageIDs []int
			for j := stageStart; j < stageEnd && j < len(row); j++ {
				cellValue := strings.TrimSpace(strings.ToLower(row[j]))
				if cellValue == "yes" || cellValue == "1" || cellValue == "true" || cellValue == "y" {
					// Get stage ID from database using combined header name
					var stageID int
					stageName := combinedHeaders[j]
					if err := gjm.db.Raw("SELECT id FROM project_stages WHERE name = ? AND project_id = ?", stageName, projectID).Scan(&stageID).Error; err == nil && stageID > 0 {
						allStageIDs = append(allStageIDs, stageID)
					}
				}
			}
			if len(allStageIDs) > 0 {
				stages = append(stages, models.Stages{
					StagePath: allStageIDs,
				})
			}
		}
		elementType.Stage = stages

		// Process drawings (columns after stages based on drawingTypeCount) - use combined headers
		var drawings []models.Drawings
		if drawingTypeCount > 0 {
			for j := drawingStart; j < drawingEnd && j < len(row); j++ {
				file := strings.TrimSpace(row[j])
				if file != "" {
					drawingTypeName := combinedHeaders[j]
					var drawingTypeID int
					if err := gjm.db.Raw("SELECT drawing_type_id FROM drawing_type WHERE drawing_type_name = ? AND project_id = ?", drawingTypeName, projectID).Scan(&drawingTypeID).Error; err == nil && drawingTypeID > 0 {
						drawings = append(drawings, models.Drawings{
							DrawingsId:      repository.GenerateRandomNumber(),
							CreatedAt:       time.Now(),
							UpdateAt:        time.Now(),
							ProjectId:       projectID,
							CurrentVersion:  "VR-1",
							File:            file,
							DrawingTypeId:   drawingTypeID,
							DrawingTypeName: drawingTypeName,
							ElementTypeID:   0, // To be set later
						})
					}
				}
			}
		}
		elementType.Drawings = drawings

		// Process paths (columns after drawings based on precastCount) - use dot notation for ltree format
		hierarchyQuantity := []models.HierarchyQuantity{}
		log.Printf("DEBUG: precastCount = %d, pathStart = %d, pathEnd = %d", precastCount, pathStart, pathEnd)
		if precastCount > 0 {
			log.Printf("DEBUG: Processing hierarchy data - precastCount > 0")
			log.Printf("=== DEBUG: Hierarchy Headers ===")
			for j := pathStart; j < pathEnd && j < len(row); j++ {
				mainHeader := ""
				subHeader := ""
				if j < len(mainHeaders) {
					mainHeader = mainHeaders[j]
				}
				if j < len(subHeaders) {
					subHeader = subHeaders[j]
				}
				log.Printf("Column %d: mainHeader='%s', subHeader='%s'", j, mainHeader, subHeader)
			}
			log.Printf("=== END DEBUG ===")

			for j := pathStart; j < pathEnd && j < len(row); j++ {
				quantity, err := strconv.Atoi(row[j])
				if err != nil || quantity == 0 {
					continue // Skip invalid or zero values
				}

				// Find the main header for this hierarchy section (look backwards from current position)
				mainHeader := ""
				for k := j; k >= pathStart; k-- {
					if k < len(mainHeaders) && mainHeaders[k] != "" {
						mainHeader = mainHeaders[k]
						break
					}
				}

				// Get the sub-header for current column
				subHeader := ""
				if j < len(subHeaders) {
					subHeader = subHeaders[j]
				}

				// Create hierarchy name with proper header/sub-header handling
				hierarchyName := createHierarchyName(mainHeader, subHeader)
				if hierarchyName == "" {
					continue // Skip if no valid name
				}

				log.Printf("DEBUG: Column %d - Header='%s', SubHeader='%s', Created hierarchy name: '%s'", j, mainHeader, subHeader, hierarchyName)

				// Try multiple variations to find the hierarchy ID
				var hierarchyID int
				var found bool
				var actualNamingConvention string

				// Sanitize hierarchy name for ltree (replace hyphens with underscores)
				sanitizedHierarchyName := sanitizeHierarchyNameForLtree(hierarchyName)

				// Try the exact sanitized name first
				if err := gjm.db.Raw("SELECT id FROM precast WHERE path = ? AND project_id = ?", sanitizedHierarchyName, projectID).Scan(&hierarchyID).Error; err == nil && hierarchyID > 0 {
					found = true
					log.Printf("Found hierarchy ID %d for exact naming convention '%s'", hierarchyID, sanitizedHierarchyName)
				} else {
					// Try with different variations (all sanitized)
					variations := generateHierarchyVariations(hierarchyName)
					log.Printf("DEBUG: Trying %d variations for hierarchy name '%s'", len(variations), hierarchyName)
					for i, variation := range variations {
						// Sanitize each variation for ltree compatibility
						sanitizedVariation := sanitizeHierarchyNameForLtree(variation)
						log.Printf("DEBUG: Trying variation %d: '%s' (sanitized: '%s')", i+1, variation, sanitizedVariation)
						if err := gjm.db.Raw("SELECT id FROM precast WHERE path = ? AND project_id = ?", sanitizedVariation, projectID).Scan(&hierarchyID).Error; err == nil && hierarchyID > 0 {
							found = true
							log.Printf("Found hierarchy ID %d for variation '%s' (original: '%s')", hierarchyID, sanitizedVariation, hierarchyName)
							break
						} else if err != nil {
							// Log ltree syntax errors gracefully without breaking the flow
							log.Printf("DEBUG: Query error for variation '%s': %v (this is expected if path doesn't exist)", sanitizedVariation, err)
						}
					}
				}

				if !found {
					log.Printf("Warning: Could not find hierarchy ID for naming convention '%s' in precast table", hierarchyName)
					continue // Skip this hierarchy entry if not found in precast table
				}

				// Fetch the actual naming_convention from the precast table
				if err := gjm.db.Raw("SELECT naming_convention FROM precast WHERE id = ? AND project_id = ?", hierarchyID, projectID).Scan(&actualNamingConvention).Error; err != nil {
					log.Printf("Warning: Could not fetch naming_convention for hierarchy ID %d, using original: %v", hierarchyID, err)
					actualNamingConvention = hierarchyName // Fallback to original if fetch fails
				}

				hierarchyQuantity = append(hierarchyQuantity, models.HierarchyQuantity{
					HierarchyId:      hierarchyID,
					Quantity:         quantity,
					NamingConvention: actualNamingConvention, // Use the actual naming_convention from database
				})
			}
		}
		elementType.HierarchyQ = hierarchyQuantity
		log.Printf("DEBUG: Final hierarchy quantity count: %d", len(hierarchyQuantity))
		if len(hierarchyQuantity) == 0 {
			log.Printf("DEBUG: No hierarchy data found - this might be the issue!")
		}
		for i, hq := range hierarchyQuantity {
			log.Printf("DEBUG: HierarchyQ[%d]: ID=%d, Quantity=%d, NamingConvention='%s'", i, hq.HierarchyId, hq.Quantity, hq.NamingConvention)
		}
		elementType.TotalCountElement = len(hierarchyQuantity)

		// Process BOM products (columns after paths) - use combined headers
		var products []models.Product
		if invBomCount > 0 {
			for j := bomStart; j < len(row) && j < len(combinedHeaders); j++ {
				quantity, err := strconv.ParseFloat(row[j], 64)
				if err != nil || quantity == 0 {
					continue
				}

				productName := combinedHeaders[j]
				if productName == "" {
					continue
				}

				var productID int
				if err := gjm.db.Raw("SELECT id FROM inv_bom WHERE name_id = ? AND project_id = ?", productName, projectID).Scan(&productID).Error; err == nil && productID > 0 {
					products = append(products, models.Product{
						ProductID:   productID,
						ProductName: productName,
						Quantity:    quantity,
					})
				}
			}
		}
		elementType.Products = products

		elementTypes = append(elementTypes, elementType)
	}

	return elementTypes, nil
}

// parseFloat safely parses a string to float64
func parseFloat(s string) float64 {
	if s == "" {
		return 0.0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0.0
	}
	return f
}

// processBatchWithGorm processes a batch of element types using GORM with better error handling
func (gjm *GormJobManager) processBatchWithGorm(ctx context.Context, batch []models.ElementType, jobID int) []string {
	var errList []string

	// Start a transaction
	tx := gjm.db.Begin()
	if tx.Error != nil {
		return []string{fmt.Sprintf("Failed to start transaction: %v", tx.Error)}
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			errList = append(errList, fmt.Sprintf("Transaction panic: %v", r))
		}
	}()

	for i, elementType := range batch {
		// Check for cancellation before processing each element
		select {
		case <-ctx.Done():
			log.Printf("Job %d cancelled during batch processing", jobID)
			return errList
		default:
			// Continue processing
		}

		// Time individual element processing
		elementStartTime := time.Now()
		// Convert to GORM model (without hierarchy_quantity field as it doesn't exist in DB)
		elementTypeGorm := models.ElementTypeGorm{
			ElementType:        elementType.ElementType,
			ElementTypeName:    elementType.ElementTypeName,
			Thickness:          fmt.Sprintf("%.2f", elementType.Thickness),
			Length:             fmt.Sprintf("%.2f", elementType.Length),
			Height:             fmt.Sprintf("%.2f", elementType.Height),
			Mass:               fmt.Sprintf("%.2f", elementType.Mass),
			Volume:             fmt.Sprintf("%.2f", elementType.Volume),
			Area:               fmt.Sprintf("%.2f", elementType.Area),
			Width:              fmt.Sprintf("%.2f", elementType.Width),
			CreatedBy:          elementType.CreatedBy,
			CreatedAt:          elementType.CreatedAt,
			UpdatedAt:          elementType.UpdatedAt,
			ProjectID:          elementType.ProjectID,
			ElementTypeVersion: elementType.ElementTypeVersion,
			TotalCountElement:  elementType.TotalCountElement,
			Density:            elementType.Density, // Keep as float64 for numeric column
			JobID:              jobID,
		}

		// Create element type
		if err := tx.Create(&elementTypeGorm).Error; err != nil {
			errList = append(errList, fmt.Sprintf("Row %d: Failed to create element type: %v", i+1, err))
			continue
		}

		// Process drawings if any
		log.Printf("Processing drawings for element type %s (ID: %d) - Found %d drawings",
			elementType.ElementType, elementTypeGorm.ElementTypeID, len(elementType.Drawings))

		if len(elementType.Drawings) == 0 {
			log.Printf("No drawings found for element type %s", elementType.ElementType)
		}

		for _, drawing := range elementType.Drawings {
			drawing.CreatedBy = elementType.CreatedBy
			drawing.UpdatedBy = elementType.CreatedBy

			drawingGorm := models.DrawingsGorm{
				ProjectId:      drawing.ProjectId,
				CurrentVersion: drawing.CurrentVersion,
				CreatedAt:      drawing.CreatedAt,
				CreatedBy:      drawing.CreatedBy,
				DrawingTypeId:  drawing.DrawingTypeId,
				UpdateAt:       drawing.UpdateAt,
				UpdatedBy:      drawing.UpdatedBy,
				Comments:       drawing.Comments,
				File:           drawing.File,
				ElementTypeID:  int(elementTypeGorm.ElementTypeID), // Set to the newly created element type ID
			}

			// Log drawing data
			log.Printf("Creating drawing for element type %s (ID: %d):", elementType.ElementType, elementTypeGorm.ElementTypeID)
			log.Printf("  Drawing Data: ProjectId=%d, CurrentVersion=%s, DrawingTypeId=%d, File=%s, Comments=%s",
				drawingGorm.ProjectId, drawingGorm.CurrentVersion, drawingGorm.DrawingTypeId, drawingGorm.File, drawingGorm.Comments)
			log.Printf("  CreatedBy=%s, UpdatedBy=%s, ElementTypeID=%d",
				drawingGorm.CreatedBy, drawingGorm.UpdatedBy, drawingGorm.ElementTypeID)

			if err := tx.Create(&drawingGorm).Error; err != nil {
				log.Printf("Error creating drawing: %v", err)
				errList = append(errList, fmt.Sprintf("Row %d: Failed to create drawing: %v", i+1, err))
				continue
			}

			log.Printf("Successfully created drawing with ID: %d", drawingGorm.DrawingsId)
		}

		// Process hierarchy data and create elements
		if len(elementType.HierarchyQ) > 0 {
			log.Printf("Processing hierarchy data for element type %s (ID: %d) - Found %d hierarchy entries",
				elementType.ElementType, elementTypeGorm.ElementTypeID, len(elementType.HierarchyQ))

			// Print hierarchy data from Excel
			log.Printf("=== HIERARCHY DATA FROM EXCEL ===")
			for idx, hq := range elementType.HierarchyQ {
				log.Printf("Hierarchy %d: ID=%d, Quantity=%d, NamingConvention='%s'",
					idx+1, hq.HierarchyId, hq.Quantity, hq.NamingConvention)
			}
			log.Printf("=== END HIERARCHY DATA ===")

			// Create element_type_hierarchy_quantity records for each hierarchy entry
			for _, hq := range elementType.HierarchyQ {
				// Use the hierarchy ID if provided, otherwise try to resolve from naming convention (precast.name)
				hierarchyID := hq.HierarchyId
				if hierarchyID == 0 && strings.TrimSpace(hq.NamingConvention) != "" {
					var resolvedID int
					if err := tx.Raw("SELECT id FROM precast WHERE name = ? AND project_id = ?",
						strings.TrimSpace(hq.NamingConvention), elementType.ProjectID).Scan(&resolvedID).Error; err == nil && resolvedID > 0 {
						hierarchyID = resolvedID
					}
				}

				if hierarchyID == 0 {
					log.Printf("Row %d: Invalid or unresolved hierarchy ID for hierarchy entry (name='%s')", i+1, hq.NamingConvention)
					continue
				}

				// Verify the hierarchy ID exists in the precast table
				var exists bool
				err := tx.Raw("SELECT EXISTS(SELECT 1 FROM precast WHERE id = ? AND project_id = ?)",
					hierarchyID, elementType.ProjectID).Scan(&exists).Error
				if err != nil || !exists {
					log.Printf("Hierarchy ID %d not found in project %d (name='%s')", hierarchyID, elementType.ProjectID, hq.NamingConvention)
					errList = append(errList, fmt.Sprintf("Row %d: Hierarchy ID %d not found in project %d", i+1, hierarchyID, elementType.ProjectID))
					continue
				}

				// Create element_type_hierarchy_quantity record
				projectID := elementTypeGorm.ProjectID
				leftQuantity := 0 // Default to zero
				hierarchyQuantity := models.ElementTypeHierarchyQuantityGorm{
					ElementTypeID:    int(elementTypeGorm.ElementTypeID),
					HierarchyId:      hierarchyID,
					Quantity:         hq.Quantity,
					NamingConvention: hq.NamingConvention,
					ElementTypeName:  elementTypeGorm.ElementTypeName,
					ElementType:      elementTypeGorm.ElementType,
					LeftQuantity:     &leftQuantity,
					ProjectID:        &projectID,
				}

				if err := tx.Create(&hierarchyQuantity).Error; err != nil {
					log.Printf("Error creating element_type_hierarchy_quantity for hierarchy ID %d: %v", hierarchyID, err)
					errList = append(errList, fmt.Sprintf("Row %d: Failed to create element_type_hierarchy_quantity for hierarchy ID %d: %v", i+1, hierarchyID, err))
					continue
				}

				log.Printf("Successfully created element_type_hierarchy_quantity for hierarchy ID %d", hierarchyID)
			}

			// Create element input data for each hierarchy entry
			for _, hq := range elementType.HierarchyQ {
				// Use the hierarchy ID that's already provided in the data
				hierarchyID := hq.HierarchyId
				if hierarchyID == 0 {
					log.Printf("Row %d: Invalid hierarchy ID for hierarchy entry", i+1)
					continue
				}

				// Verify the hierarchy ID exists in the precast table
				var exists bool
				err := tx.Raw("SELECT EXISTS(SELECT 1 FROM precast WHERE id = ? AND project_id = ?)",
					hierarchyID, elementType.ProjectID).Scan(&exists).Error
				if err != nil || !exists {
					log.Printf("Hierarchy ID %d not found in project %d", hierarchyID, elementType.ProjectID)
					errList = append(errList, fmt.Sprintf("Row %d: Hierarchy ID %d not found in project %d", i+1, hierarchyID, elementType.ProjectID))
					continue
				}

				// Create element input data
				elementData := models.ElementInput{
					HierarchyId:        hierarchyID,
					Quantity:           hq.Quantity,
					NamingConvention:   "default",     // Use default naming convention
					SessionID:          "gorm-import", // Use a default session ID for GORM imports
					ElementTypeID:      int(elementTypeGorm.ElementTypeID),
					ProjectID:          elementType.ProjectID,
					ElementType:        elementType.ElementType,
					ElementTypeName:    elementType.ElementTypeName,
					ElementTypeVersion: elementType.ElementTypeVersion,
					TotalCountElement:  elementType.TotalCountElement,
				}

				log.Printf("Creating %d elements for hierarchy ID %d",
					hq.Quantity, hierarchyID)

				// Create elements using GORM
				if err := gjm.createElementsWithGorm(tx, elementData); err != nil {
					log.Printf("Error creating elements for hierarchy ID %d: %v", hierarchyID, err)
					errList = append(errList, fmt.Sprintf("Row %d: Failed to create elements for hierarchy ID %d: %v", i+1, hierarchyID, err))
					continue
				}

				log.Printf("Successfully created %d elements for hierarchy ID %d", hq.Quantity, hierarchyID)
			}
		} else {
			log.Printf("No hierarchy data found for element type %s", elementType.ElementType)
		}

		// Create element_type_path records if stage data exists
		if len(elementType.Stage) > 0 {
			stage := elementType.Stage[0]

			// Use raw SQL with pq.Array for proper PostgreSQL array handling
			stageQuery := `INSERT INTO element_type_path (element_type_id, stage_path) VALUES ($1, $2)`
			if err := tx.Exec(stageQuery, int(elementTypeGorm.ElementTypeID), pq.Array(stage.StagePath)).Error; err != nil {
				log.Printf("Error creating element_type_path: %v", err)
				errList = append(errList, fmt.Sprintf("Row %d: Failed to create element_type_path: %v", i+1, err))
			} else {
				log.Printf("Successfully created element_type_path for element type %s with stage path: %v", elementType.ElementType, stage.StagePath)
			}
		}

		// Create element_type_bom records if product data exists
		if len(elementType.Products) > 0 {
			log.Printf("Processing BOM data for element type %s (ID: %d) - Found %d products",
				elementType.ElementType, elementTypeGorm.ElementTypeID, len(elementType.Products))

			// Print BOM data from Excel
			log.Printf("=== BOM DATA FROM EXCEL ===")
			for idx, product := range elementType.Products {
				log.Printf("BOM %d: ProductID=%d, ProductName='%s', Quantity=%.2f",
					idx+1, product.ProductID, product.ProductName, product.Quantity)
			}
			log.Printf("=== END BOM DATA ===")

			for _, product := range elementType.Products {
				elementTypeBom := models.ElementTypeBomGorm{
					ElementTypeID: int(elementTypeGorm.ElementTypeID),
					ProjectID:     elementType.ProjectID,
					ProductID:     product.ProductID,
					ProductName:   product.ProductName,
					Quantity:      product.Quantity,
					Unit:          "",  // Default empty unit
					Rate:          0.0, // Default zero rate
					CreatedAt:     time.Now(),
					CreatedBy:     elementType.CreatedBy,
					UpdatedAt:     time.Now(),
					UpdatedBy:     elementType.CreatedBy,
				}

				if err := tx.Create(&elementTypeBom).Error; err != nil {
					log.Printf("Error creating element_type_bom for product %d: %v", product.ProductID, err)
					errList = append(errList, fmt.Sprintf("Row %d: Failed to create element_type_bom for product %d: %v", i+1, product.ProductID, err))
				} else {
					log.Printf("Successfully created element_type_bom for element type %s, product %d", elementType.ElementType, product.ProductID)
				}
			}
		}

		// Log element processing time
		elementTime := time.Since(elementStartTime)
		log.Printf("Processed element type '%s' in %v", elementType.ElementType, elementTime)
	}

	// Commit transaction if no errors
	if len(errList) == 0 {
		if err := tx.Commit().Error; err != nil {
			return []string{fmt.Sprintf("Failed to commit transaction: %v", err)}
		}
	} else {
		tx.Rollback()
	}

	return errList
}

// createElementsWithGorm creates elements using GORM within a transaction
func (gjm *GormJobManager) createElementsWithGorm(tx *gorm.DB, elementData models.ElementInput) error {
	// Get the current total count for this element type
	var currentTotalCount int
	if err := tx.Raw("SELECT total_count_element FROM element_type WHERE element_type_id = ?", elementData.ElementTypeID).Scan(&currentTotalCount).Error; err != nil {
		return fmt.Errorf("failed to get current total count: %v", err)
	}

	// Create elements
	for i := 1; i <= elementData.Quantity; i++ {
		elementID := repository.GenerateElementID(elementData.ElementType, elementData.NamingConvention, currentTotalCount+i)

		element := models.ElementGorm{
			ElementTypeID:      elementData.ElementTypeID,
			ElementId:          elementID,
			ElementName:        elementData.ElementTypeName,
			ProjectID:          elementData.ProjectID,
			CreatedBy:          "gorm-import", // Use default value for GORM imports
			CreatedAt:          time.Now(),
			Status:             "1", // fresh status
			ElementTypeVersion: elementData.ElementTypeVersion,
			UpdateAt:           time.Now(),
			TargetLocation:     elementData.HierarchyId,
		}

		if err := tx.Create(&element).Error; err != nil {
			return fmt.Errorf("failed to create element %d: %v", i, err)
		}
	}

	// Update the total_count_element in element_type table
	newTotalCount := currentTotalCount + elementData.Quantity
	if err := tx.Model(&models.ElementTypeGorm{}).Where("element_type_id = ?", elementData.ElementTypeID).Update("total_count_element", newTotalCount).Error; err != nil {
		return fmt.Errorf("failed to update total count: %v", err)
	}

	return nil
}

// ImportElementTypeExcelHandlerGorm handles Excel import using GORM
func (gjm *GormJobManager) ImportElementTypeExcelHandlerGorm(c *gin.Context) {
	// Check if job creation is blocked (inspired by reference code)
	if gjm.IsJobCreationBlocked() {
		log.Printf("Job creation blocked - rejecting new import request")
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Job creation is currently blocked due to system termination"})
		return
	}

	// Note: Database schema check removed - using element_type_hierarchy_quantity table

	sessionID := c.GetHeader("Authorization")
	if sessionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "session_id header is missing"})
		return
	}

	// Get session details
	sqlDB := storage.GetDB()
	session, userName, err := GetSessionDetails(sqlDB, sessionID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
		return
	}

	// Set request body size limit
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20) // 10 MB

	// Get project_id from URL parameter
	projectIDStr := c.Param("project_id")
	if projectIDStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "project_id is required"})
		return
	}

	projectID, err := strconv.Atoi(projectIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project_id"})
		return
	}

	// Get the file from the request
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File upload failed"})
		return
	}

	// Upload file to server
	importDir := "./imports/"
	filePath, err := UploadFileToDirectory(file, importDir, 10<<20) // 10 MB limit
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to upload file to server"})
		return
	}

	// Create a new import job and get job ID with file path
	jobID, err := gjm.CreateImportJobAndGetID(c, &filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create job"})
		return
	}

	// Check if job is already running (inspired by reference code)
	if gjm.IsJobRunning(jobID) {
		log.Printf("Job %d already running", jobID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Job already running"})
		return
	}

	// Get batch size from query parameter, default to 30
	batchSizeStr := c.DefaultQuery("batch_size", "30")
	batchSize, err := strconv.Atoi(batchSizeStr)
	if err != nil || batchSize < 1 || batchSize > 50 {
		batchSize = 30 // Default to 30 if invalid
	}

	// Get concurrent batches from query parameter, default to 15
	concurrentBatchesStr := c.DefaultQuery("concurrent_batches", "15")
	concurrentBatches, err := strconv.Atoi(concurrentBatchesStr)
	if err != nil || concurrentBatches < 1 || concurrentBatches > 20 {
		concurrentBatches = 15 // Default to 15 if invalid
	}

	// Return job ID immediately to prevent timeout
	c.JSON(http.StatusOK, gin.H{
		"message":            "Import job started successfully",
		"job_id":             jobID,
		"status":             "pending",
		"batch_size":         batchSize,
		"concurrent_batches": concurrentBatches,
		"file_path":          filePath,
		"handler":            "gorm",
	})

	// Log activity in background
	go func() {
		activityLog := models.ActivityLog{
			EventContext: "Import Element Type (GORM)",
			EventName:    "Import",
			Description:  "Import Element Type Excel using GORM",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}

		if logErr := SaveActivityLog(sqlDB, activityLog); logErr != nil {
			log.Printf("Failed to log activity: %v", logErr)
		}
	}()

	// Create a unified job management approach
	// First, register the job BEFORE starting any processing
	ctx, cancel := context.WithCancel(context.Background())

	// Register job immediately with proper tracking
	gjm.registerJob(jobID, cancel)
	log.Printf("Job %d registered for unified management", jobID)

	// Verify registration immediately
	if gjm.IsJobRunning(jobID) {
		log.Printf("Job %d successfully registered and is running", jobID)
	} else {
		log.Printf("ERROR: Job %d registration failed - not found in running jobs", jobID)
	}

	// Start background processing with unified management
	go func() {
		defer func() {
			gjm.unregisterJob(jobID)
			log.Printf("Job %d completed and unregistered", jobID)
		}()

		// Check if job was terminated before starting
		if gjm.IsJobTerminated(jobID) {
			log.Printf("Job %d was terminated, not starting processing", jobID)
			return
		}

		// Check if job creation is blocked
		if gjm.IsJobCreationBlocked() {
			log.Printf("Job creation blocked - not starting processing for job %d", jobID)
			return
		}

		// Check if context is already cancelled
		select {
		case <-ctx.Done():
			log.Printf("Job %d context already cancelled, not starting processing", jobID)
			return
		default:
			// Continue processing
		}

		// Use enhanced cancellation method for better goroutine cleanup
		gjm.ProcessElementTypeExcelImportJobFromPathWithEnhancedCancellation(ctx, jobID, projectID, filePath, batchSize, concurrentBatches, userName, session)
	}()
}

// TerminateJobAndRollback terminates a job and rolls back its data using GORM
// @Summary Terminate job and rollback
// @Description Terminate a job and rollback its data
// @Tags Jobs
// @Accept json
// @Produce json
// @Param job_id path int true "Job ID"
// @Success 200 {object} models.RollbackResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/jobs/{job_id}/terminate [delete]
func (gjm *GormJobManager) TerminateJobAndRollback(c *gin.Context) {
	// Add panic recovery
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Termination panic recovered: %v", r)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Termination failed due to internal error"})
		}
	}()

	jobIDStr := c.Param("job_id")
	if jobIDStr == "" {
		log.Printf("Termination failed: job_id is required")
		c.JSON(http.StatusBadRequest, gin.H{"error": "job_id is required"})
		return
	}

	jobID, err := strconv.Atoi(jobIDStr)
	if err != nil {
		log.Printf("Termination failed: invalid job_id: %s", jobIDStr)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job_id"})
		return
	}

	log.Printf("=== JOB LIFECYCLE: Termination request received for job %d ===", jobID)

	// Get job details
	var job models.ImportJobGorm
	if err := gjm.db.First(&job, jobID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			log.Printf("Termination failed: Job %d not found in database", jobID)
			c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
			return
		}
		log.Printf("Termination failed: Database error fetching job %d: %v", jobID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch job"})
		return
	}

	log.Printf("JOB LIFECYCLE: Found job in database - ID=%d, Type=%s, Status=%s", job.ID, job.JobType, job.Status)

	// Check if job can be terminated
	if job.Status == "completed" {
		log.Printf("JOB LIFECYCLE: Cannot terminate completed job %d", jobID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Cannot terminate completed job"})
		return
	}

	// Unified termination approach - Step 1: Mark job as terminated immediately
	log.Printf("JOB LIFECYCLE: Step 1 - Marking job %d as terminated", jobID)
	gjm.MarkJobAsTerminated(jobID)
	gjm.SetGlobalTerminationFlag()
	log.Printf("JOB LIFECYCLE: Job %d marked as terminated and global flag set", jobID)

	// Step 2: Use unified state management to cancel the job
	log.Printf("JOB LIFECYCLE: Step 2 - Attempting to cancel job %d using unified state management", jobID)
	if gjm.cancelJobState(jobID) {
		log.Printf("JOB LIFECYCLE: Successfully cancelled job %d using unified state management", jobID)
	} else {
		log.Printf("JOB LIFECYCLE: Failed to cancel job %d using unified state management", jobID)

		// Fallback: Try to cancel using the old jobCancelMap
		log.Printf("JOB LIFECYCLE: Attempting fallback cancellation using jobCancelMap for job %d", jobID)
		gjm.jobMutex.RLock()
		if cancelFunc, exists := gjm.jobCancelMap[jobID]; exists && cancelFunc != nil {
			log.Printf("JOB LIFECYCLE: Found job %d in jobCancelMap, cancelling", jobID)
			cancelFunc()
			log.Printf("JOB LIFECYCLE: Successfully cancelled job %d using jobCancelMap", jobID)
		} else {
			log.Printf("JOB LIFECYCLE: Job %d not found in either unified state or jobCancelMap", jobID)
		}
		gjm.jobMutex.RUnlock()
	}

	// Step 2.5: Comprehensive job status verification
	log.Printf("JOB LIFECYCLE: Step 2.5 - Comprehensive job status verification for job %d", jobID)
	gjm.verifyJobStatus(jobID)

	// Step 2.5: Force immediate context cancellation for all running jobs
	gjm.jobMutex.RLock()
	for runningJobID, cancelFunc := range gjm.jobCancelMap {
		if runningJobID == jobID && cancelFunc != nil {
			log.Printf("Force cancelling context for job %d", jobID)
			cancelFunc() // Force cancel the context immediately
			break
		}
	}
	gjm.jobMutex.RUnlock()

	// Also try the old method as fallback
	gjm.jobMutex.RLock()
	cancelFunc, exists := gjm.jobCancelMap[jobID]
	gjm.jobMutex.RUnlock()

	if exists && cancelFunc != nil {
		log.Printf("Found cancel function for job %d in old system, calling it", jobID)
		cancelFunc() // Cancel the context immediately
		log.Printf("Cancel function called for job %d via old system", jobID)
	} else {
		log.Printf("No cancel function found for job %d in old system", jobID)
	}

	// Step 3: Immediately update job status to cancelled in database
	log.Printf("JOB LIFECYCLE: Step 3 - Updating job %d status to cancelled in database", jobID)
	immediateUpdate := map[string]interface{}{
		"status":       "cancelled",
		"updated_at":   time.Now(),
		"completed_at": time.Now(),
		"error":        "Job terminated by user",
		"progress":     0,
	}

	if err := gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Updates(immediateUpdate).Error; err != nil {
		log.Printf("Failed to immediately update job %d status: %v", jobID, err)
	} else {
		log.Printf("Job %d status immediately updated to cancelled", jobID)
	}

	// Send immediate response to prevent timeout
	c.JSON(http.StatusOK, gin.H{
		"message":      "Job termination initiated",
		"job_id":       jobID,
		"status":       "terminating",
		"job_type":     job.JobType,
		"project_id":   job.ProjectID,
		"cancelled_at": time.Now().Format("2006-01-02 15:04:05"),
	})

	// Continue termination process in background
	go func() {
		log.Printf("Starting background termination for job %d", jobID)

		// Force stop the job immediately using the tracking system
		jobStopped := gjm.ForceStopJob(jobID)
		log.Printf("Force stop result for job %d: %v", jobID, jobStopped)

		// Wait for job to fully stop
		log.Printf("Waiting for job %d to fully stop...", jobID)
		time.Sleep(200 * time.Millisecond) // Give time for cleanup

		// Double-check if job is still running and force stop again
		if gjm.IsJobRunning(jobID) {
			log.Printf("Job %d still running after force stop, attempting final cleanup", jobID)
			gjm.StopSpecificJob(jobID)
			time.Sleep(100 * time.Millisecond) // Additional wait
		}

		// Ensure job is unregistered from tracking
		gjm.unregisterJob(jobID)
		log.Printf("Background termination completed for job %d", jobID)
	}()

	// Continue rollback process in background
	go func() {
		// Force immediate status update to cancelled regardless of stop result
		time.Sleep(500 * time.Millisecond)
		log.Printf("Job %d forcing immediate status update to cancelled", jobID)

		forceUpdate := map[string]interface{}{
			"status":       "cancelled",
			"updated_at":   time.Now(),
			"completed_at": time.Now(),
			"error":        "Job force-cancelled by user",
			"progress":     0,
		}

		if err := gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Updates(forceUpdate).Error; err != nil {
			log.Printf("Failed to force update job %d status: %v", jobID, err)
		} else {
			log.Printf("Job %d status force-updated to cancelled", jobID)
		}

		// Wait a moment for the job to respond to cancellation (with timeout)
		select {
		case <-time.After(2 * time.Second):
			log.Printf("Job %d termination timeout, proceeding with rollback", jobID)
		case <-time.After(500 * time.Millisecond):
			// Normal wait time
		}

		// Verify job status after stopping
		var updatedJob models.ImportJobGorm
		if err := gjm.db.First(&updatedJob, jobID).Error; err == nil {
			log.Printf("Job %d status after stop: %s", jobID, updatedJob.Status)

			// If job is still running, update status to cancelled anyway
			if updatedJob.Status == "processing" || updatedJob.Status == "pending" {
				log.Printf("Job %d still running after stop attempts, updating status to cancelled", jobID)

				// Update status to cancelled even if processes are still running
				emergencyUpdate := map[string]interface{}{
					"status":       "cancelled",
					"updated_at":   time.Now(),
					"completed_at": time.Now(),
					"error":        "Job cancelled by user - processes may still be stopping",
					"progress":     0,
				}

				if emergencyErr := gjm.db.Model(&updatedJob).Updates(emergencyUpdate).Error; emergencyErr != nil {
					log.Printf("Failed to update job %d status: %v", jobID, emergencyErr)
				} else {
					log.Printf("Successfully updated job %d status to cancelled", jobID)
				}
			}
		}

		// Start transaction for rollback
		tx := gjm.db.Begin()
		if tx.Error != nil {
			log.Printf("Termination failed: Failed to start transaction for job %d: %v", jobID, tx.Error)
			return
		}

		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic during rollback: %v", r)
				tx.Rollback()
			}
		}()

		// Track rollback statistics
		rollbackStats := map[string]int{
			"element_types":           0,
			"elements":                0,
			"element_type_quantities": 0,
			"drawings":                0,
			"element_type_boms":       0,
			"element_type_paths":      0,
		}

		// First, get all element_type_id values created by this job
		var elementTypeIDs []int
		if err := tx.Model(&models.ElementTypeGorm{}).Select("element_type_id").Where("job_id = ?", jobID).Find(&elementTypeIDs).Error; err != nil {
			log.Printf("Termination failed: Error fetching element type IDs for job %d: %v", jobID, err)
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch element types for rollback"})
			return
		}

		log.Printf("Found %d element types to rollback for job %d: %v", len(elementTypeIDs), jobID, elementTypeIDs)

		// Perform comprehensive rollback based on job type and element type relationships
		switch job.JobType {
		case "element_type_import":
			if len(elementTypeIDs) > 0 {
				// Delete elements related to these element types
				elementsResult := tx.Where("element_type_id IN ?", elementTypeIDs).Delete(&models.ElementGorm{})
				if elementsResult.Error != nil {
					log.Printf("Error rolling back elements: %v", elementsResult.Error)
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rollback elements"})
					return
				}
				rollbackStats["elements"] = int(elementsResult.RowsAffected)

				// Delete element type quantities related to these element types
				quantitiesResult := tx.Where("element_type_id IN ?", elementTypeIDs).Delete(&models.ElementTypeQuantityGorm{})
				if quantitiesResult.Error != nil {
					log.Printf("Error rolling back element type quantities: %v", quantitiesResult.Error)
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rollback element type quantities"})
					return
				}
				rollbackStats["element_type_quantities"] = int(quantitiesResult.RowsAffected)

				// Delete element type BOMs related to these element types
				bomsResult := tx.Where("element_type_id IN ?", elementTypeIDs).Delete(&models.ElementTypeBomGorm{})
				if bomsResult.Error != nil {
					log.Printf("Error rolling back element type BOMs: %v", bomsResult.Error)
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rollback element type BOMs"})
					return
				}
				rollbackStats["element_type_boms"] = int(bomsResult.RowsAffected)

				// Delete element type paths related to these element types
				pathsResult := tx.Where("element_type_id IN ?", elementTypeIDs).Delete(&models.ElementTypePathGorm{})
				if pathsResult.Error != nil {
					log.Printf("Error rolling back element type paths: %v", pathsResult.Error)
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rollback element type paths"})
					return
				}
				rollbackStats["element_type_paths"] = int(pathsResult.RowsAffected)

				// Delete drawings related to these element types
				drawingsResult := tx.Where("element_type_id IN ?", elementTypeIDs).Delete(&models.DrawingsGorm{})
				if drawingsResult.Error != nil {
					log.Printf("Error rolling back drawings: %v", drawingsResult.Error)
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rollback drawings"})
					return
				}
				rollbackStats["drawings"] = int(drawingsResult.RowsAffected)
			}

			// Delete element types created by this job (do this last due to foreign key constraints)
			elementTypesResult := tx.Where("job_id = ?", jobID).Delete(&models.ElementTypeGorm{})
			if elementTypesResult.Error != nil {
				log.Printf("Error rolling back element types: %v", elementTypesResult.Error)
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to rollback element types"})
				return
			}
			rollbackStats["element_types"] = int(elementTypesResult.RowsAffected)

		default:
			// Handle other job types - use the same relationship-based approach
			log.Printf("Handling generic job type: %s", job.JobType)

			if len(elementTypeIDs) > 0 {
				// Delete related entities first
				if result := tx.Where("element_type_id IN ?", elementTypeIDs).Delete(&models.ElementGorm{}); result.Error != nil {
					log.Printf("Error rolling back elements for generic job: %v", result.Error)
				} else {
					rollbackStats["elements"] = int(result.RowsAffected)
				}

				if result := tx.Where("element_type_id IN ?", elementTypeIDs).Delete(&models.DrawingsGorm{}); result.Error != nil {
					log.Printf("Error rolling back drawings for generic job: %v", result.Error)
				} else {
					rollbackStats["drawings"] = int(result.RowsAffected)
				}
			}

			// Delete element types created by this job
			if result := tx.Where("job_id = ?", jobID).Delete(&models.ElementTypeGorm{}); result.Error != nil {
				log.Printf("Error rolling back element types for generic job: %v", result.Error)
			} else {
				rollbackStats["element_types"] = int(result.RowsAffected)
			}
		}

		// Update job status to cancelled
		cancelTime := time.Now()
		updateData := map[string]interface{}{
			"status":       "cancelled",
			"updated_at":   cancelTime,
			"completed_at": &cancelTime,
			"error":        "Job cancelled by user - all data rolled back",
			"progress":     0,
		}

		log.Printf("Updating job %d status to cancelled", jobID)
		if err := tx.Model(&job).Updates(updateData).Error; err != nil {
			log.Printf("Error updating job status for job %d: %v", jobID, err)
			log.Printf("Update data: %+v", updateData)
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update job status"})
			return
		}

		// Commit transaction
		if err := tx.Commit().Error; err != nil {
			log.Printf("Error committing transaction: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		// Log rollback summary
		log.Printf("=== ROLLBACK SUMMARY FOR JOB %d ===", jobID)
		log.Printf("Job Type: %s", job.JobType)
		log.Printf("Project ID: %d", job.ProjectID)
		totalRolledBack := 0
		for entity, count := range rollbackStats {
			if count > 0 {
				log.Printf("Rolled back %s: %d", entity, count)
				totalRolledBack += count
			}
		}
		log.Printf("Total records rolled back: %d", totalRolledBack)
		log.Printf("=====================================")

		// Final verification after rollback
		log.Printf("JOB LIFECYCLE: Final verification after rollback for job %d", jobID)
		gjm.verifyJobStatus(jobID)

		// Log completion instead of sending HTTP response
		log.Printf("Background rollback completed for job %d", jobID)
		log.Printf("=== JOB LIFECYCLE: Termination and rollback completed for job %d ===", jobID)
	}() // Close the second go func()
}

// GetPendingAndProcessingJobs returns only the latest job ID using GORM (within 30 minutes)
// @Summary Get pending and processing jobs
// @Description Get pending and processing jobs for a project within the last 30 minutes
// @Tags Jobs
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Success 200 {array} models.JobResponse
// @Failure 500 {object} models.ErrorResponse
// @Router /api/jobs/pending-processing/{project_id} [get]
func GetPendingAndProcessingJobsGorm(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Calculate cutoff time (30 minutes ago)
		cutoffTime := time.Now().Add(-30 * time.Minute)

		var job models.ImportJobGorm
		if err := db.Where("status IN ? AND created_at > ?", []string{"pending", "processing"}, cutoffTime).Order("created_at DESC").First(&job).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusOK, gin.H{
					"jobs": []gin.H{},
				})
				return
			}
			log.Printf("Error fetching latest pending/processing job: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch latest job"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"jobs": []gin.H{
				{
					"job_id":           job.ID,
					"job_type":         job.JobType,
					"status":           job.Status,
					"project_id":       job.ProjectID,
					"created_at":       job.CreatedAt.Format("2006-01-02 15:04:05"),
					"rollback_enabled": job.RollbackEnabled,
				},
			},
		})
	}
}

// HybridJobManager combines both SQL and GORM approaches
type HybridJobManager struct {
	sqlDB  *sql.DB
	gormDB *gorm.DB
}

// NewHybridJobManager creates a new hybrid job manager
func NewHybridJobManager() *HybridJobManager {
	return &HybridJobManager{
		sqlDB:  storage.GetDB(),
		gormDB: storage.GetGormDB(),
	}
}

// CreateJobWithHybrid creates a job using GORM but allows SQL operations
func (hjm *HybridJobManager) CreateJobWithHybrid(c *gin.Context, filePath *string) (int, error) {
	// Use GORM for job creation
	gormJM := NewGormJobManager()
	return gormJM.CreateImportJobAndGetID(c, filePath)
}

// GetJobStatusWithHybrid gets job status using GORM
func (hjm *HybridJobManager) GetJobStatusWithHybrid(c *gin.Context) {
	gormJM := NewGormJobManager()
	gormJM.GetJobStatus(c)
}

// UpdateJobStatusWithHybrid updates job status using GORM
func (hjm *HybridJobManager) UpdateJobStatusWithHybrid(jobID int, status string, progress int, processedItems int, errorMsg *string, result *string) error {
	gormJM := NewGormJobManager()
	return gormJM.UpdateJobStatus(jobID, status, progress, processedItems, errorMsg, result)
}

// EnableRollbackForJob enables rollback for a specific job
// @Summary Enable rollback for job
// @Description Enable rollback functionality for a specific job
// @Tags Jobs
// @Accept json
// @Produce json
// @Param job_id path int true "Job ID"
// @Success 200 {object} models.SuccessResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/jobs/{job_id}/enable-rollback [post]
func EnableRollbackForJob(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get job ID from URL parameter
		jobIDStr := c.Param("job_id")
		jobID, err := strconv.Atoi(jobIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job ID"})
			return
		}

		// Start a transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start transaction"})
			return
		}

		// Defer rollback in case of error
		defer func() {
			if err != nil {
				tx.Rollback()
			}
		}()

		// Check if job exists
		var jobExists bool
		err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM import_jobs WHERE id = $1)`, jobID).Scan(&jobExists)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check job existence: " + err.Error()})
			return
		}

		if !jobExists {
			c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
			return
		}

		// Enable rollback for the job
		_, err = tx.Exec(`UPDATE import_jobs SET rollback_enabled = true WHERE id = $1`, jobID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to enable rollback: " + err.Error()})
			return
		}

		// Commit the transaction
		err = tx.Commit()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction: " + err.Error()})
			return
		}

		// Return success response
		c.JSON(http.StatusOK, gin.H{
			"message":   "Rollback enabled successfully for job",
			"job_id":    jobID,
			"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		})
	}
}

// RollbackAllElementTypeDataWithGorm rolls back element type data for a specific project and job
// @Summary Rollback element type data
// @Description Rollback element type data for a specific project and job
// @Tags Jobs
// @Accept json
// @Produce json
// @Param project_id path int true "Project ID"
// @Param job_id path int true "Job ID"
// @Success 200 {object} models.RollbackResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 404 {object} models.ErrorResponse
// @Router /api/rollback/element_type/{project_id}/{job_id} [post]
func RollbackAllElementTypeDataWithGorm(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get project ID from URL parameter
		projectIDStr := c.Param("project_id")
		projectID, err := strconv.Atoi(projectIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid project ID"})
			return
		}

		// Get job ID from URL parameter
		jobIDStr := c.Param("job_id")
		jobID, err := strconv.Atoi(jobIDStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job ID"})
			return
		}

		// Start a transaction for rollback operation
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to start transaction"})
			return
		}

		// Defer rollback in case of error
		defer func() {
			if err != nil {
				tx.Rollback()
			}
		}()

		// Get session information for audit trail
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session ID is required"})
			return
		}

		var createdBy string
		err = tx.QueryRow(`SELECT host_name FROM session WHERE session_id = $1`, sessionID).Scan(&createdBy)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid session ID: user not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "database error while retrieving session"})
			return
		}

		// Check if rollback is enabled for this job
		var rollbackEnabled bool
		err = tx.QueryRow(`SELECT rollback_enabled FROM import_jobs WHERE id = $1`, jobID).Scan(&rollbackEnabled)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check rollback status: " + err.Error()})
			return
		}

		if !rollbackEnabled {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   "Rollback is not enabled for this job",
				"job_id":  jobID,
				"message": "Rollback must be enabled before performing rollback operation",
			})
			return
		}

		// Log the rollback operation
		log.Printf("User %s initiated rollback of element type data for project ID: %d, job ID: %d", createdBy, projectID, jobID)

		// First, get all element_type_ids for this job
		var elementTypeIDs []int
		elementTypeRows, err := tx.Query(`SELECT element_type_id FROM element_type WHERE project_id = $1 AND job_id = $2`, projectID, jobID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get element_type_ids: " + err.Error()})
			return
		}
		defer elementTypeRows.Close()

		for elementTypeRows.Next() {
			var elementTypeID int
			if err := elementTypeRows.Scan(&elementTypeID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to scan element_type_id: " + err.Error()})
				return
			}
			elementTypeIDs = append(elementTypeIDs, elementTypeID)
		}

		if len(elementTypeIDs) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "No element types found for this job"})
			return
		}

		log.Printf("Found %d element types for job %d: %v", len(elementTypeIDs), jobID, elementTypeIDs)

		// Create safe placeholders for IN clause
		placeholders := make([]string, len(elementTypeIDs))
		args := make([]interface{}, len(elementTypeIDs))
		for i, id := range elementTypeIDs {
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = id
		}
		inClause := strings.Join(placeholders, ",")

		// Run validation checks sequentially (transaction safety)
		log.Printf("Running validation checks for job %d", jobID)

		// Check production status
		var productionCount int
		err = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM element e WHERE e.instage = true AND e.element_type_id IN (%s)`, inClause),
			args...).Scan(&productionCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to check production status: %v", err)})
			return
		}
		if productionCount > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   fmt.Sprintf("Rollback denied: %d production operations are in progress for job ID %d", productionCount, jobID),
				"count":   productionCount,
				"type":    "production",
				"job_id":  jobID,
				"message": "Cannot rollback element types while production operations are active",
			})
			return
		}

		// Check activity status
		var activityCount int
		err = tx.QueryRow(fmt.Sprintf(`
			SELECT COUNT(*) FROM activity a
			JOIN task t ON a.task_id = t.task_id
			WHERE a.status IN ('in_progress', 'started', 'active') AND t.element_type_id IN (%s)`, inClause),
			args...).Scan(&activityCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to check activity status: %v", err)})
			return
		}
		if activityCount > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   fmt.Sprintf("Rollback denied: %d activity operations are in progress for job ID %d", activityCount, jobID),
				"count":   activityCount,
				"type":    "activity",
				"job_id":  jobID,
				"message": "Cannot rollback element types while activity operations are active",
			})
			return
		}

		// Check task status
		var taskCount int
		err = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM task WHERE status IN ('in_progress', 'started', 'active') AND element_type_id IN (%s)`, inClause),
			args...).Scan(&taskCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to check task status: %v", err)})
			return
		}
		if taskCount > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   fmt.Sprintf("Rollback denied: %d task operations are in progress for job ID %d", taskCount, jobID),
				"count":   taskCount,
				"type":    "task",
				"job_id":  jobID,
				"message": "Cannot rollback element types while task operations are active",
			})
			return
		}

		// Check stockyard status
		var stockyardCount int
		err = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM precast_stock WHERE stockyard = true AND element_type_id IN (%s)`, inClause),
			args...).Scan(&stockyardCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to check stockyard status: %v", err)})
			return
		}
		if stockyardCount > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":   fmt.Sprintf("Rollback denied: %d stockyard operations are in progress for job ID %d", stockyardCount, jobID),
				"count":   stockyardCount,
				"type":    "stockyard",
				"job_id":  jobID,
				"message": "Cannot rollback element types while stockyard operations are active",
			})
			return
		}

		log.Printf("All validation checks passed for job %d", jobID)

		// Get counts sequentially for reporting (transaction safety)
		log.Printf("Getting count information for job %d", jobID)

		countMap := make(map[string]int)

		// Count element types
		countMap["element_types"] = len(elementTypeIDs)

		// Count element_type_quantity records
		var elementTypeQuantityCount int
		err = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM element_type_quantity WHERE element_type_id IN (%s)`, inClause),
			args...).Scan(&elementTypeQuantityCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to count element_type_quantity: %v", err)})
			return
		}
		countMap["element_type_quantity"] = elementTypeQuantityCount

		// Count drawings
		var drawingsCount int
		err = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM drawings WHERE element_type_id IN (%s)`, inClause),
			args...).Scan(&drawingsCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to count drawings: %v", err)})
			return
		}
		countMap["drawings"] = drawingsCount

		// Count element_type_path records
		var elementTypePathCount int
		err = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM element_type_path WHERE element_type_id IN (%s)`, inClause),
			args...).Scan(&elementTypePathCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to count element_type_path: %v", err)})
			return
		}
		countMap["element_type_path"] = elementTypePathCount

		// Count element_type_bom records
		var bomCount int
		err = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM element_type_bom WHERE element_type_id IN (%s)`, inClause),
			args...).Scan(&bomCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to count bom: %v", err)})
			return
		}
		countMap["bom"] = bomCount

		// Count elements
		var elementsCount int
		err = tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM element WHERE element_type_id IN (%s)`, inClause),
			args...).Scan(&elementsCount)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to count elements: %v", err)})
			return
		}
		countMap["elements"] = elementsCount

		log.Printf("Count summary for job %d: %+v", jobID, countMap)

		// Use PostgreSQL array syntax for efficient bulk deletions
		// Delete related records using element_type_id array for better performance
		// Delete drawings_revision first (foreign key dependency)
		_, err = tx.Exec(fmt.Sprintf(`
			DELETE FROM drawings_revision 
			WHERE parent_drawing_id IN (SELECT drawing_id FROM drawings WHERE element_type_id IN (%s))
		`, inClause), args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete drawings_revision: " + err.Error()})
			return
		}

		// Delete drawings
		_, err = tx.Exec(fmt.Sprintf(`DELETE FROM drawings WHERE element_type_id IN (%s)`, inClause), args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete drawings: " + err.Error()})
			return
		}

		// Delete element_type_quantity records
		_, err = tx.Exec(fmt.Sprintf(`DELETE FROM element_type_quantity WHERE element_type_id IN (%s)`, inClause), args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete element_type_quantity: " + err.Error()})
			return
		}

		// Delete element_type_path records
		_, err = tx.Exec(fmt.Sprintf(`DELETE FROM element_type_path WHERE element_type_id IN (%s)`, inClause), args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete element_type_path: " + err.Error()})
			return
		}

		// Delete element_type_bom records
		_, err = tx.Exec(fmt.Sprintf(`DELETE FROM element_type_bom WHERE element_type_id IN (%s)`, inClause), args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete element_type_bom: " + err.Error()})
			return
		}

		// Delete element_type_revision records
		_, err = tx.Exec(fmt.Sprintf(`DELETE FROM element_type_revision WHERE element_type_id IN (%s)`, inClause), args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete element_type_revision: " + err.Error()})
			return
		}

		// Delete elements
		_, err = tx.Exec(fmt.Sprintf(`DELETE FROM element WHERE element_type_id IN (%s)`, inClause), args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete elements: " + err.Error()})
			return
		}

		// Finally, delete all element_type records for this job
		finalResult, err := tx.Exec(`DELETE FROM element_type WHERE project_id = $1 AND job_id = $2`, projectID, jobID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete element_type: " + err.Error()})
			return
		}

		// Get the number of deleted records
		rowsAffected, err := finalResult.RowsAffected()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get rows affected: " + err.Error()})
			return
		}

		// Update rollback_enabled to false after successful rollback
		_, err = tx.Exec(`UPDATE import_jobs SET rollback_enabled = false WHERE id = $1`, jobID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update rollback status: " + err.Error()})
			return
		}

		// Commit the transaction
		err = tx.Commit()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to commit transaction: " + err.Error()})
			return
		}

		// Log successful rollback
		log.Printf("Successfully rolled back %d element type records and all related data for project ID: %d, job ID: %d", rowsAffected, projectID, jobID)

		// Return success response with detailed information
		c.JSON(http.StatusOK, gin.H{
			"message":        fmt.Sprintf("Successfully rolled back element type data for project ID %d, job ID %d. %d element types and all related data have been removed.", projectID, jobID, rowsAffected),
			"deleted_count":  rowsAffected,
			"project_id":     projectID,
			"job_id":         jobID,
			"rolled_back_by": createdBy,
			"timestamp":      time.Now().Format("2006-01-02 15:04:05"),
			"deleted_records_summary": gin.H{
				"element_types":           countMap["element_types"],
				"element_type_quantities": countMap["element_type_quantity"],
				"drawings":                countMap["drawings"],
				"element_type_paths":      countMap["element_type_path"],
				"boms":                    countMap["bom"],
				"elements":                countMap["elements"],
			},
		})
	}
}

// ProcessElementTypeImportJobWithEnhancedCancellation processes the element type import with enhanced cancellation support
func (gjm *GormJobManager) ProcessElementTypeImportJobWithEnhancedCancellation(ctx context.Context, jobID int, projectID int, elementTypes []models.ElementType, batchSize int, concurrentBatches int) {
	// Update job status to processing
	err := gjm.UpdateJobStatus(jobID, "processing", 0, 0, nil, nil)
	if err != nil {
		log.Printf("Error updating job status to processing: %v", err)
		return
	}

	totalElements := len(elementTypes)

	// Update total items
	err = gjm.UpdateJobTotalItems(jobID, totalElements)
	if err != nil {
		log.Printf("Error updating job total items: %v", err)
		return
	}

	// Process elements in batches with enhanced cancellation
	processedItems := 0
	successCount := 0
	errorCount := 0
	var errors []string

	// Create a ticker for periodic progress updates
	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()

	// Create a channel for progress updates
	progressChan := make(chan struct{})
	defer close(progressChan)

	// Start progress updater goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-progressChan:
				return
			case <-progressTicker.C:
				// Update progress in DB
				progress := (processedItems * 100) / totalElements
				if updateErr := gjm.UpdateJobStatus(jobID, "processing", progress, processedItems, nil, nil); updateErr != nil {
					log.Printf("Error updating job progress: %v", updateErr)
				}
			}
		}
	}()

	for i := 0; i < totalElements; i += batchSize {
		// Enhanced cancellation check before each batch
		select {
		case <-ctx.Done():
			log.Printf("Job %d cancelled during processing", jobID)
			errorMsg := "Job cancelled by user"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		default:
			// Continue processing
		}

		end := i + batchSize
		if end > totalElements {
			end = totalElements
		}

		batch := elementTypes[i:end]

		// Process batch with enhanced cancellation
		batchErrors := gjm.processBatchWithEnhancedCancellation(ctx, batch, jobID)

		// Check if batch was cancelled
		if len(batchErrors) > 0 && batchErrors[0] == "cancelled" {
			log.Printf("Job %d cancelled during batch processing", jobID)
			errorMsg := "Job cancelled by user"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			// Reset global flags when job is cancelled
			gjm.ResetGlobalTerminationFlag()
			gjm.UnblockJobCreation()
			return
		}

		if len(batchErrors) > 0 {
			errors = append(errors, batchErrors...)
			errorCount += len(batchErrors)
		} else {
			successCount += len(batch)
		}

		processedItems += len(batch)
		progress := (processedItems * 100) / totalElements

		// Update progress
		updateErr := gjm.UpdateJobStatus(jobID, "processing", progress, processedItems, nil, nil)
		if updateErr != nil {
			log.Printf("Error updating job progress: %v", updateErr)
		}

		// Log batch progress
		log.Printf("Processed batch %d/%d: %d items, %d errors",
			(i/batchSize)+1, (totalElements+batchSize-1)/batchSize, len(batch), len(batchErrors))

		// Additional cancellation check after each batch
		select {
		case <-ctx.Done():
			log.Printf("Job %d cancelled after batch processing", jobID)
			errorMsg := "Job cancelled by user"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			// Reset global flags when job is cancelled
			gjm.ResetGlobalTerminationFlag()
			gjm.UnblockJobCreation()
			return
		default:
			// Continue processing
		}

		// Check for global termination after batch
		if gjm.IsGlobalTerminationSet() {
			log.Printf("Job %d global termination detected after batch", jobID)
			errorMsg := "Job cancelled due to global termination"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			// Reset global flags when job is cancelled
			gjm.ResetGlobalTerminationFlag()
			gjm.UnblockJobCreation()
			return
		}

		// Check if job was terminated after batch
		if gjm.IsJobTerminated(jobID) {
			log.Printf("Job %d termination detected after batch", jobID)
			errorMsg := "Job terminated"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			// Reset global flags when job is cancelled
			gjm.ResetGlobalTerminationFlag()
			gjm.UnblockJobCreation()
			return
		}
	}

	// Determine final status and result
	var finalStatus string
	var resultMsg string
	var errorMsg *string

	if errorCount == 0 {
		finalStatus = "completed"
		resultMsg = fmt.Sprintf("Successfully processed all %d elements", successCount)
	} else if successCount == 0 {
		finalStatus = "failed"
		errorMsgStr := fmt.Sprintf("Failed to process any elements. Errors: %v", errors)
		errorMsg = &errorMsgStr
	} else {
		finalStatus = "completed_with_errors"
		errorDetails := strings.Join(errors, "\n")
		resultMsg = fmt.Sprintf("Processed %d elements successfully, %d errors occurred", successCount, errorCount)
		errorMsg = &errorDetails
	}

	// Update final job status
	gjm.UpdateJobStatus(jobID, finalStatus, 100, processedItems, errorMsg, &resultMsg)
}

// processBatchWithEnhancedCancellation processes a batch with enhanced cancellation support
func (gjm *GormJobManager) processBatchWithEnhancedCancellation(ctx context.Context, batch []models.ElementType, jobID int) []string {
	var errList []string

	// Check for cancellation before starting batch
	select {
	case <-ctx.Done():
		log.Printf("Job %d cancelled before batch processing", jobID)
		return []string{"cancelled"}
	default:
		// Continue processing
	}

	// Check if job was terminated
	if gjm.IsJobTerminated(jobID) {
		log.Printf("Job %d terminated before batch processing", jobID)
		return []string{"cancelled"}
	}

	// Check if global termination is set
	if gjm.IsGlobalTerminationSet() {
		log.Printf("Job %d global termination before batch processing", jobID)
		return []string{"cancelled"}
	}

	// Start a transaction
	tx := gjm.db.Begin()
	if tx.Error != nil {
		return []string{fmt.Sprintf("Failed to start transaction: %v", tx.Error)}
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			errList = append(errList, fmt.Sprintf("Transaction panic: %v", r))
		}
	}()

	for i, elementType := range batch {
		// Check for cancellation before processing each element
		select {
		case <-ctx.Done():
			log.Printf("Job %d cancelled during batch processing", jobID)
			tx.Rollback()
			return append(errList, "cancelled")
		default:
			// Continue processing
		}

		// Check if job was terminated
		if gjm.IsJobTerminated(jobID) {
			log.Printf("Job %d terminated during batch processing", jobID)
			tx.Rollback()
			return append(errList, "cancelled")
		}

		// Check if global termination is set
		if gjm.IsGlobalTerminationSet() {
			log.Printf("Job %d global termination during batch processing", jobID)
			tx.Rollback()
			return append(errList, "cancelled")
		}

		// Check if job is still running (verification)
		if !gjm.IsJobRunning(jobID) {
			log.Printf("Job %d is not running during batch processing", jobID)
			tx.Rollback()
			return append(errList, "cancelled")
		}

		// Process individual element with cancellation check
		if err := gjm.processElementWithCancellation(ctx, tx, elementType, jobID); err != nil {
			if err.Error() == "cancelled" {
				log.Printf("Job %d cancelled during element processing", jobID)
				tx.Rollback()
				return append(errList, "cancelled")
			}
			errList = append(errList, fmt.Sprintf("Element %d: %v", i, err))
		}

		// Additional cancellation check after each element
		select {
		case <-ctx.Done():
			log.Printf("Job %d cancelled after element processing", jobID)
			tx.Rollback()
			return append(errList, "cancelled")
		default:
			// Continue processing
		}
	}

	// Check for cancellation before committing
	select {
	case <-ctx.Done():
		log.Printf("Job %d cancelled before commit", jobID)
		tx.Rollback()
		return append(errList, "cancelled")
	default:
		// Continue processing
	}

	// Commit transaction if no cancellation occurred
	if err := tx.Commit().Error; err != nil {
		return append(errList, fmt.Sprintf("Failed to commit transaction: %v", err))
	}

	return errList
}

// processElementWithCancellation processes a single element with cancellation support
func (gjm *GormJobManager) processElementWithCancellation(ctx context.Context, tx *gorm.DB, elementType models.ElementType, jobID int) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return fmt.Errorf("cancelled")
	default:
		// Continue processing
	}
	// Check if job was terminated
	if gjm.IsJobTerminated(jobID) {
		return fmt.Errorf("cancelled")
	}
	// Check if global termination is set
	if gjm.IsGlobalTerminationSet() {
		return fmt.Errorf("cancelled")
	}

	// Convert to GORM model
	elementTypeGorm := models.ElementTypeGorm{
		ElementType:        elementType.ElementType,
		ElementTypeName:    elementType.ElementTypeName,
		Thickness:          fmt.Sprintf("%.2f", elementType.Thickness),
		Length:             fmt.Sprintf("%.2f", elementType.Length),
		Height:             fmt.Sprintf("%.2f", elementType.Height),
		Mass:               fmt.Sprintf("%.2f", elementType.Mass),
		Volume:             fmt.Sprintf("%.2f", elementType.Volume),
		Area:               fmt.Sprintf("%.2f", elementType.Area),
		Width:              fmt.Sprintf("%.2f", elementType.Width),
		CreatedBy:          elementType.CreatedBy,
		CreatedAt:          elementType.CreatedAt,
		UpdatedAt:          elementType.UpdatedAt,
		ProjectID:          elementType.ProjectID,
		ElementTypeVersion: elementType.ElementTypeVersion,
		TotalCountElement:  elementType.TotalCountElement,
		Density:            elementType.Density,
		JobID:              jobID,
	}

	// Check for cancellation before database operations
	select {
	case <-ctx.Done():
		return fmt.Errorf("cancelled")
	default:
		// Continue processing
	}

	// 🚨 IMMEDIATE TERMINATION CHECKS - Before element creation
	if gjm.IsJobTerminated(jobID) {
		log.Printf("🚨 IMMEDIATE TERMINATION: Job %d terminated - stopping element creation", jobID)
		return fmt.Errorf("cancelled")
	}

	if gjm.IsGlobalTerminationSet() {
		log.Printf("🚨 IMMEDIATE TERMINATION: Global termination set - stopping element creation for job %d", jobID)
		return fmt.Errorf("cancelled")
	}

	// Check database status for immediate termination
	var dbJobStatus string
	if err := gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Select("status").Scan(&dbJobStatus).Error; err == nil {
		if dbJobStatus == "cancelled" || dbJobStatus == "terminated" {
			log.Printf("🚨 IMMEDIATE TERMINATION: Job %d status is %s in database, stopping element creation", jobID, dbJobStatus)
			return fmt.Errorf("cancelled")
		}
	}

	// Create element type
	if err := tx.Create(&elementTypeGorm).Error; err != nil {
		return fmt.Errorf("failed to create element type: %v", err)
	}

	// Process related data with cancellation checks
	if err := gjm.processElementRelatedData(ctx, tx, elementType, elementTypeGorm.ElementTypeID, jobID); err != nil {
		return fmt.Errorf("failed to process related data: %v", err)
	}

	return nil
}

// processElementRelatedData processes drawings, quantities, and BOM data with cancellation support
func (gjm *GormJobManager) processElementRelatedData(ctx context.Context, tx *gorm.DB, elementType models.ElementType, elementTypeID int, jobID int) error {
	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		return fmt.Errorf("cancelled")
	default:
		// Continue processing
	}

	// Check if job was terminated - we need to pass jobID as parameter
	// This will be handled at the calling level
	// But we can still check global termination here
	if gjm.IsGlobalTerminationSet() {
		return fmt.Errorf("cancelled")
	}

	// Process drawings
	for _, drawing := range elementType.Drawings {
		// Check for cancellation before each drawing
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled")
		default:
			// Continue processing
		}
		// Check if global termination is set
		if gjm.IsGlobalTerminationSet() {
			return fmt.Errorf("cancelled")
		}

		// Check if job is still running (verification)
		if !gjm.IsJobRunning(jobID) {
			return fmt.Errorf("cancelled")
		}

		// Check if job was terminated (immediate check)
		if gjm.IsJobTerminated(jobID) {
			return fmt.Errorf("cancelled")
		}

		// Check if job was terminated (immediate check)
		if gjm.IsJobTerminated(jobID) {
			return fmt.Errorf("cancelled")
		}

		// Check if global termination is set (immediate check)
		if gjm.IsGlobalTerminationSet() {
			return fmt.Errorf("cancelled")
		}

		drawingGorm := models.DrawingsGorm{
			DrawingsId:      uint(drawing.DrawingsId),
			CreatedAt:       drawing.CreatedAt,
			UpdateAt:        drawing.UpdateAt,
			ProjectId:       drawing.ProjectId,
			CurrentVersion:  drawing.CurrentVersion,
			File:            drawing.File,
			DrawingTypeId:   drawing.DrawingTypeId,
			DrawingTypeName: drawing.DrawingTypeName,
			ElementTypeID:   elementTypeID,
		}

		// 🚨 IMMEDIATE TERMINATION CHECKS - Before every database operation
		if gjm.IsJobTerminated(jobID) {
			log.Printf("🚨 IMMEDIATE TERMINATION: Job %d terminated - stopping database operation", jobID)
			return fmt.Errorf("cancelled")
		}

		if gjm.IsGlobalTerminationSet() {
			log.Printf("🚨 IMMEDIATE TERMINATION: Global termination set - stopping database operation for job %d", jobID)
			return fmt.Errorf("cancelled")
		}

		// Check database status for immediate termination
		var dbJobStatus string
		if err := gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Select("status").Scan(&dbJobStatus).Error; err == nil {
			if dbJobStatus == "cancelled" || dbJobStatus == "terminated" {
				log.Printf("🚨 IMMEDIATE TERMINATION: Job %d status is %s in database, stopping database operation", jobID, dbJobStatus)
				return fmt.Errorf("cancelled")
			}
		}

		if err := tx.Create(&drawingGorm).Error; err != nil {
			return fmt.Errorf("failed to create drawing: %v", err)
		}
	}

	// Process hierarchy quantities (store as JSON in ElementTypeGorm)
	if len(elementType.HierarchyQ) > 0 {
		// Check for cancellation before processing hierarchy
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled")
		default:
			// Continue processing
		}
		// Check if global termination is set
		if gjm.IsGlobalTerminationSet() {
			return fmt.Errorf("cancelled")
		}

		// Check if job is still running (verification)
		if !gjm.IsJobRunning(jobID) {
			return fmt.Errorf("cancelled")
		}

		// Check if job was terminated (immediate check)
		if gjm.IsJobTerminated(jobID) {
			return fmt.Errorf("cancelled")
		}

		// Debug: Log the hierarchy data before marshaling
		log.Printf("Element %d: HierarchyQ data: %+v", elementTypeID, elementType.HierarchyQ)

		hierarchyJSON, err := json.Marshal(elementType.HierarchyQ)
		if err != nil {
			log.Printf("Element %d: JSON marshal error: %v", elementTypeID, err)
			return fmt.Errorf("failed to marshal hierarchy quantity: %v", err)
		}

		// Debug: Log the JSON string
		log.Printf("Element %d: Generated JSON: %s", elementTypeID, string(hierarchyJSON))

		// Check for immediate termination before database operation
		if gjm.IsJobTerminated(jobID) {
			return fmt.Errorf("cancelled")
		}

		if gjm.IsGlobalTerminationSet() {
			return fmt.Errorf("cancelled")
		}

		// Note: Hierarchy data is now stored in element_type_hierarchy_quantity table
		// No need to update the element_type table with hierarchy_quantity column
	}

	// Process BOM products (store as JSON in ElementTypeBomGorm)
	if len(elementType.Products) > 0 {
		log.Printf("=== OLD FUNCTION - BOM DATA FROM EXCEL ===")
		for idx, product := range elementType.Products {
			log.Printf("BOM %d: ProductID=%d, ProductName='%s', Quantity=%.2f",
				idx+1, product.ProductID, product.ProductName, product.Quantity)
		}
		log.Printf("=== END OLD FUNCTION BOM DATA ===")
		// Check for cancellation before processing BOM
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled")
		default:
			// Continue processing
		}

		// Check if global termination is set
		if gjm.IsGlobalTerminationSet() {
			return fmt.Errorf("cancelled")
		}

		// Check if job is still running (verification)
		if !gjm.IsJobRunning(jobID) {
			return fmt.Errorf("cancelled")
		}

		// Check if job was terminated (immediate check)
		if gjm.IsJobTerminated(jobID) {
			return fmt.Errorf("cancelled")
		}

		// Create BOM records for each product
		for _, product := range elementType.Products {
			bomGorm := models.ElementTypeBomGorm{
				ElementTypeID: elementTypeID,
				ProjectID:     elementType.ProjectID,
				ProductID:     product.ProductID,
				ProductName:   product.ProductName,
				Quantity:      product.Quantity,
				Unit:          "",  // Default empty unit
				Rate:          0.0, // Default zero rate
				CreatedAt:     time.Now(),
				CreatedBy:     elementType.CreatedBy,
				UpdatedAt:     time.Now(),
				UpdatedBy:     elementType.CreatedBy,
			}

			// Check for immediate termination before database operation
			if gjm.IsJobTerminated(jobID) {
				return fmt.Errorf("cancelled")
			}

			if gjm.IsGlobalTerminationSet() {
				return fmt.Errorf("cancelled")
			}

			if err := tx.Create(&bomGorm).Error; err != nil {
				return fmt.Errorf("failed to create BOM for product %d: %v", product.ProductID, err)
			}
		}
	}

	// Process hierarchy data to create elements
	if len(elementType.HierarchyQ) > 0 {
		// Check for cancellation before processing elements
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled")
		default:
			// Continue processing
		}

		log.Printf("Processing hierarchy data for element type %d: %+v", elementTypeID, elementType.HierarchyQ)

		// Print hierarchy data from Excel in old function
		log.Printf("=== OLD FUNCTION - HIERARCHY DATA FROM EXCEL ===")
		for idx, hq := range elementType.HierarchyQ {
			log.Printf("Hierarchy %d: ID=%d, Quantity=%d, NamingConvention='%s'",
				idx+1, hq.HierarchyId, hq.Quantity, hq.NamingConvention)
		}
		log.Printf("=== END OLD FUNCTION HIERARCHY DATA ===")

		// // Create element_type_hierarchy_quantity records for each hierarchy entry (OLD FUNCTION)
		for _, hq := range elementType.HierarchyQ {
			// Use the hierarchy ID if provided, otherwise try to resolve from naming convention (precast.name)
			hierarchyID := hq.HierarchyId
			if hierarchyID == 0 && strings.TrimSpace(hq.NamingConvention) != "" {
				var resolvedID int
				if err := tx.Raw("SELECT id FROM precast WHERE name = ? AND project_id = ?",
					strings.TrimSpace(hq.NamingConvention), elementType.ProjectID).Scan(&resolvedID).Error; err == nil && resolvedID > 0 {
					hierarchyID = resolvedID
				}
			}

			if hierarchyID == 0 {
				log.Printf("Invalid or unresolved hierarchy ID for hierarchy entry (name='%s')", hq.NamingConvention)
				continue
			}

			// Verify the hierarchy ID exists in the precast table
			var exists bool
			err := tx.Raw("SELECT EXISTS(SELECT 1 FROM precast WHERE id = ? AND project_id = ?)",
				hierarchyID, elementType.ProjectID).Scan(&exists).Error
			if err != nil || !exists {
				log.Printf("Hierarchy ID %d not found in project %d (name='%s')", hierarchyID, elementType.ProjectID, hq.NamingConvention)
				continue
			}

			// Create element_type_hierarchy_quantity record
			projectID := elementType.ProjectID
			leftQuantity := 0 // Default to zero
			hierarchyQuantity := models.ElementTypeHierarchyQuantityGorm{
				ElementTypeID:    int(elementTypeID),
				HierarchyId:      hierarchyID,
				Quantity:         hq.Quantity,
				NamingConvention: hq.NamingConvention,
				ElementTypeName:  elementType.ElementTypeName,
				ElementType:      elementType.ElementType,
				LeftQuantity:     &leftQuantity,
				ProjectID:        &projectID,
			}

			if err := tx.Create(&hierarchyQuantity).Error; err != nil {
				log.Printf("Error creating element_type_hierarchy_quantity for hierarchy ID %d: %v", hierarchyID, err)
				continue
			}

			log.Printf("Successfully created element_type_hierarchy_quantity for hierarchy ID %d", hierarchyID)
		}

		// Create element_type_quantity records and elements for each hierarchy entry
		// ✅ Print the total count of HierarchyQ
		log.Printf("Total hierarchies: %d", len(elementType.HierarchyQ))

		for i, hq := range elementType.HierarchyQ {
			hierarchyID := hq.HierarchyId
			if hierarchyID == 0 {
				log.Printf("[Index %d] ❌ Invalid hierarchy ID (0) for hierarchy entry", i)
				continue
			}

			// Verify hierarchy ID exists for the same project
			var exists bool
			err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM precast WHERE id = ? AND project_id = ?)`,
				hierarchyID, elementType.ProjectID).Scan(&exists).Error
			if err != nil || !exists {
				log.Printf("[Index %d] ⚠️ Hierarchy ID %d not found in project %d", i, hierarchyID, elementType.ProjectID)
				continue
			}

			// Get parent ID
			var parentID sql.NullInt64
			err = tx.Raw("SELECT parent_id FROM precast WHERE id = ?", hierarchyID).Scan(&parentID).Error
			if err != nil {
				log.Printf("[Index %d] ⚠️ Error fetching parent ID for hierarchy ID %d: %v", i, hierarchyID, err)
				continue
			}

			// ⚠️ Skip top-level hierarchies (no parent)
			if !parentID.Valid {
				log.Printf("[Index %d] ⏩ Skipping top-level hierarchy ID %d (no parent)", i, hierarchyID)
				continue
			}

			towerID := int(parentID.Int64)

			// ✅ Skip self-referencing hierarchy
			if towerID == hierarchyID {
				log.Printf("[Index %d] ⚠️ Skipping self-referencing hierarchy (tower=%d, floor=%d)", i, towerID, hierarchyID)
				continue
			}

			// Prepare insert data
			elementQuantity := models.ElementTypeQuantityGorm{
				Tower:           towerID,
				Floor:           hierarchyID,
				ElementTypeName: elementType.ElementTypeName,
				ElementType:     elementType.ElementType,
				ElementTypeID:   int(elementTypeID),
				TotalQuantity:   hq.Quantity,
				LeftQuantity:    0,
				ProjectID:       elementType.ProjectID,
			}

			// Insert record
			if err := tx.Create(&elementQuantity).Error; err != nil {
				log.Printf("[Index %d] ❌ Error creating element_type_quantity for hierarchy ID %d: %v", i, hierarchyID, err)
				continue
			}

			log.Printf("[Index %d] ✅ Successfully inserted element_type_quantity for hierarchy ID %d", i, hierarchyID)
		}

		for _, hq := range elementType.HierarchyQ {
			// Check for cancellation before each hierarchy
			select {
			case <-ctx.Done():
				return fmt.Errorf("cancelled")
			default:
				// Continue processing
			}

			// Get naming convention from precast table
			var namingConvention string
			err := tx.Raw("SELECT naming_convention FROM precast WHERE id = ?", hq.HierarchyId).Scan(&namingConvention).Error
			if err != nil {
				log.Printf("Error fetching naming convention for hierarchy_id %d: %v", hq.HierarchyId, err)
				continue
			}

			// Create elements for this hierarchy
			for i := 1; i <= hq.Quantity; i++ {
				// Check for cancellation before each element creation
				select {
				case <-ctx.Done():
					return fmt.Errorf("cancelled")
				default:
					// Continue processing
				}

				// Generate element ID using the naming convention
				elementID := repository.GenerateElementID(elementType.ElementType, namingConvention, i)

				element := models.ElementGorm{
					ElementTypeID:      elementTypeID,
					ElementId:          elementID,
					ElementName:        elementType.ElementTypeName,
					ProjectID:          elementType.ProjectID,
					CreatedBy:          elementType.CreatedBy,
					CreatedAt:          time.Now(),
					Status:             "1", // fresh status
					ElementTypeVersion: elementType.ElementTypeVersion,
					UpdateAt:           time.Now(),
					TargetLocation:     hq.HierarchyId,
				}

				if err := tx.Create(&element).Error; err != nil {
					log.Printf("Error creating element %d for hierarchy %d: %v", i, hq.HierarchyId, err)
					// Don't continue, return error to abort transaction
					return fmt.Errorf("failed to create element: %v", err)
				}
			}

			log.Printf("Created %d elements for hierarchy %d", hq.Quantity, hq.HierarchyId)
		}
	}

	// Process element type paths (stage paths)
	if len(elementType.Stage) > 0 {
		// Check for cancellation before processing stage paths
		select {
		case <-ctx.Done():
			return fmt.Errorf("cancelled")
		default:
			// Continue processing
		}

		log.Printf("Processing stage paths for element type %d: %+v", elementTypeID, elementType.Stage)

		for _, stage := range elementType.Stage {
			// Check for cancellation before each stage
			select {
			case <-ctx.Done():
				return fmt.Errorf("cancelled")
			default:
				// Continue processing
			}

			if len(stage.StagePath) > 0 {
				// Use raw SQL to properly handle PostgreSQL array type
				stagePathArray := pq.Array(stage.StagePath)

				if err := tx.Exec("INSERT INTO element_type_path (element_type_id, stage_path) VALUES (?, ?)",
					elementTypeID, stagePathArray).Error; err != nil {
					log.Printf("Error creating element type path for element type %d: %v", elementTypeID, err)
					// Don't continue, return error to abort transaction
					return fmt.Errorf("failed to create element type path: %v", err)
				}

				log.Printf("Created element type path for element type %d with %d stages", elementTypeID, len(stage.StagePath))
			}
		}
	}

	// ...existing code...

	return nil
}

// ProcessJobWithHybridApproach combines enhanced cancellation with fallback
func (gjm *GormJobManager) ProcessJobWithHybridApproach(ctx context.Context, jobID int, projectID int, elementTypes []models.ElementType, batchSize int, concurrentBatches int) {
	// Use enhanced cancellation for better reliability
	gjm.ProcessElementTypeImportJobWithEnhancedCancellation(ctx, jobID, projectID, elementTypes, batchSize, concurrentBatches)
}

// CancelJobWithFallback provides multiple cancellation strategies
func (gjm *GormJobManager) CancelJobWithFallback(jobID int) error {
	// Strategy 1: Try graceful cancellation first
	if gjm.stopJob(jobID) {
		log.Printf("Job %d cancelled gracefully", jobID)
		return nil
	}

	// Strategy 2: Force database rollback if graceful fails
	if err := gjm.forceJobRollback(jobID); err != nil {
		log.Printf("Forced rollback for job %d: %v", jobID, err)
		return err
	}

	log.Printf("Job %d cancelled with fallback", jobID)
	return nil
}

// forceJobRollback performs emergency rollback for stuck jobs
func (gjm *GormJobManager) forceJobRollback(jobID int) error {
	// Update job status to cancelled
	now := time.Now()
	updates := map[string]interface{}{
		"status":       "cancelled",
		"updated_at":   now,
		"completed_at": &now,
		"error":        "Job force-cancelled due to timeout",
		"progress":     0,
	}

	return gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Updates(updates).Error
}

// JobProcessingConfig holds configuration for job processing
type JobProcessingConfig struct {
	UseEnhancedCancellation bool
	ShutdownTimeout         time.Duration
	BatchSize               int
	ConcurrentBatches       int
	EnableFallback          bool
}

// DefaultJobConfig returns default configuration
func DefaultJobConfig() *JobProcessingConfig {
	return &JobProcessingConfig{
		UseEnhancedCancellation: true, // Use enhanced by default
		ShutdownTimeout:         20 * time.Second,
		BatchSize:               30,
		ConcurrentBatches:       15,
		EnableFallback:          true,
	}
}

// ProcessJobWithConfig processes jobs based on configuration
func (gjm *GormJobManager) ProcessJobWithConfig(ctx context.Context, jobID int, projectID int, elementTypes []models.ElementType, config *JobProcessingConfig) {
	if config == nil {
		config = DefaultJobConfig()
	}

	if config.UseEnhancedCancellation {
		// Use enhanced cancellation for better reliability
		gjm.ProcessElementTypeImportJobWithEnhancedCancellation(ctx, jobID, projectID, elementTypes, config.BatchSize, config.ConcurrentBatches)
	} else {
		// Use basic processing for simple jobs
		gjm.ProcessElementTypeImportJob(ctx, jobID, projectID, elementTypes, config.BatchSize, config.ConcurrentBatches)
	}
}

// ProcessElementTypeExcelImportJobFromPathWithEnhancedCancellation processes Excel import with enhanced cancellation support
func (gjm *GormJobManager) ProcessElementTypeExcelImportJobFromPathWithEnhancedCancellation(ctx context.Context, jobID int, projectID int, filePath string, batchSize int, concurrentBatches int, userName string, session models.Session) {
	log.Printf("🚨 JOB START: Processing job %d with enhanced cancellation", jobID)

	// Check if job was terminated before starting
	if gjm.IsJobTerminated(jobID) {
		log.Printf("🚨 PREEMPTIVE TERMINATION: Job %d was terminated, stopping processing", jobID)
		return
	}

	// Check if job creation is blocked
	if gjm.IsJobCreationBlocked() {
		log.Printf("Job creation blocked - stopping processing for job %d", jobID)
		return
	}

	// Check if global termination is set
	if gjm.IsGlobalTerminationSet() {
		log.Printf("Global termination set - stopping processing for job %d", jobID)
		return
	}

	// Check if job status is already cancelled in database
	var jobStatus string
	if err := gjm.db.Model(&models.ImportJobGorm{}).Select("status").Where("id = ?", jobID).Scan(&jobStatus).Error; err == nil {
		if jobStatus == "cancelled" || jobStatus == "terminated" {
			log.Printf("Job %d status is %s, stopping processing", jobID, jobStatus)
			return
		}
	}

	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		log.Printf("Job %d context already cancelled, stopping processing", jobID)
		return
	default:
		// Continue processing
	}

	// Check if job was terminated (double-check)
	if gjm.IsJobTerminated(jobID) {
		log.Printf("Job %d was terminated, stopping processing", jobID)
		return
	}

	// Check if global termination is set (double-check)
	if gjm.IsGlobalTerminationSet() {
		log.Printf("Global termination set - stopping processing for job %d", jobID)
		return
	}

	// Check if job is still running (verification)
	if !gjm.IsJobRunning(jobID) {
		log.Printf("Job %d is not running, stopping processing", jobID)
		return
	}

	// Check if job was terminated (immediate check)
	if gjm.IsJobTerminated(jobID) {
		log.Printf("Job %d was terminated, stopping processing", jobID)
		return
	}

	// Check if global termination is set (immediate check)
	if gjm.IsGlobalTerminationSet() {
		log.Printf("Global termination set - stopping processing for job %d", jobID)
		return
	}

	// Check if context has error
	if ctx.Err() != nil {
		log.Printf("Job %d context has error: %v, stopping processing", jobID, ctx.Err())
		return
	}

	// Start timing the entire process
	startTime := time.Now()
	log.Printf("🚨 JOB TIMING: Starting import job %d at %s", jobID, startTime.Format("2006-01-02 15:04:05"))

	// Add panic recovery to handle forced termination
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Job %d panic recovered: %v", jobID, r)
			errorMsg := "Job terminated due to panic"
			gjm.UpdateJobStatus(jobID, "cancelled", 0, 0, &errorMsg, nil)
		}
	}()

	// Update job status to processing
	err := gjm.UpdateJobStatus(jobID, "processing", 0, 0, nil, nil)
	if err != nil {
		log.Printf("Error updating job status to processing: %v", err)
		return
	}

	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		log.Printf("Job %d cancelled before starting", jobID)
		errorMsg := "Job cancelled by user"
		gjm.UpdateJobStatus(jobID, "terminated", 0, 0, &errorMsg, nil)
		return
	default:
		// Continue processing
	}

	// Add a goroutine to monitor cancellation more aggressively
	cancellationMonitor := make(chan struct{})
	go func() {
		<-ctx.Done()
		log.Printf("Job %d cancellation detected by monitor", jobID)
		errorMsg := "Job cancelled by user"
		gjm.UpdateJobStatus(jobID, "cancelled", 0, 0, &errorMsg, nil)
		close(cancellationMonitor)
	}()
	defer close(cancellationMonitor)

	// Check if file exists
	if !FileExists(filePath) {
		errorMsg := fmt.Sprintf("File not found at path: %s", filePath)
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	// Open Excel file from server path
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		errorMsg := fmt.Sprintf("Unable to open Excel file: %v", err)
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}
	defer f.Close()

	// Check for cancellation after file operations
	select {
	case <-ctx.Done():
		log.Printf("Job %d cancelled after file operations", jobID)
		errorMsg := "Job cancelled by user"
		gjm.UpdateJobStatus(jobID, "terminated", 0, 0, &errorMsg, nil)
		return
	default:
		// Continue processing
	}

	// Add frequent cancellation checks throughout processing
	cancellationTicker := time.NewTicker(25 * time.Millisecond) // Even more frequent checks
	defer cancellationTicker.Stop()

	// Enhanced cancellation monitoring with immediate response
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Printf("Job %d cancellation detected by monitor", jobID)
				errorMsg := "Job cancelled by user"
				gjm.UpdateJobStatus(jobID, "cancelled", 0, 0, &errorMsg, nil)
				return // Exit goroutine gracefully
			case <-cancellationTicker.C:
				// Check if context is done
				select {
				case <-ctx.Done():
					log.Printf("Job %d cancellation detected by ticker", jobID)
					errorMsg := "Job cancelled by user"
					gjm.UpdateJobStatus(jobID, "cancelled", 0, 0, &errorMsg, nil)
					return // Exit goroutine gracefully
				default:
					// Continue monitoring
				}

				// Check for global termination flag
				if gjm.IsGlobalTerminationSet() {
					log.Printf("Job %d global termination detected by monitor", jobID)
					errorMsg := "Job cancelled due to global termination"
					gjm.UpdateJobStatus(jobID, "cancelled", 0, 0, &errorMsg, nil)
					return // Exit goroutine gracefully
				}

				// Check if job was terminated
				if gjm.IsJobTerminated(jobID) {
					log.Printf("Job %d termination detected by monitor", jobID)
					errorMsg := "Job terminated"
					gjm.UpdateJobStatus(jobID, "cancelled", 0, 0, &errorMsg, nil)
					return // Exit goroutine gracefully
				}

				// Check job status in database
				var jobStatus string
				if err := gjm.db.Model(&models.ImportJobGorm{}).Select("status").Where("id = ?", jobID).Scan(&jobStatus).Error; err == nil {
					if jobStatus == "cancelled" || jobStatus == "terminated" {
						log.Printf("Job %d database status is %s, stopping processing", jobID, jobStatus)
						errorMsg := "Job cancelled in database"
						gjm.UpdateJobStatus(jobID, "cancelled", 0, 0, &errorMsg, nil)
						return // Exit goroutine gracefully
					}
				}
			}
		}
	}()

	// Get all sheets
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		errorMsg := "No sheets found in Excel file"
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	// Look for "Element Types" sheet
	elementTypesSheet := "Element Types"
	sheetFound := false
	for _, sheet := range sheets {
		if sheet == elementTypesSheet {
			sheetFound = true
			break
		}
	}

	if !sheetFound {
		errorMsg := fmt.Sprintf("Sheet '%s' not found in Excel file", elementTypesSheet)
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	// Read data from Excel sheet
	rows, err := f.GetRows(elementTypesSheet)
	if err != nil {
		errorMsg := fmt.Sprintf("Error reading Excel sheet: %v", err)
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	if len(rows) < 2 {
		errorMsg := "Excel file must have at least a header row and one data row"
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	// Check for cancellation before parsing
	select {
	case <-ctx.Done():
		log.Printf("Job %d cancelled before parsing", jobID)
		errorMsg := "Job cancelled by user"
		gjm.UpdateJobStatus(jobID, "terminated", 0, 0, &errorMsg, nil)
		return
	default:
		// Continue processing
	}

	// Parse summary sheet ranges first
	ranges, summaryErr := gjm.parseSummarySheetRanges(f, projectID)
	var elementTypes []models.ElementType
	var parseErr error

	if summaryErr != nil {
		// Fallback to old method if summary sheet parsing fails
		log.Printf("Warning: Could not parse summary sheet, using fallback method: %v", summaryErr)
		elementTypes, parseErr = gjm.parseExcelDataToElementTypes(rows, projectID, userName)
	} else {
		// Use new parsing method with summary sheet ranges
		elementTypes, parseErr = gjm.parseExcelDataToElementTypesWithRanges(rows, ranges, projectID, userName)
	}

	if parseErr != nil {
		errorMsg := fmt.Sprintf("Error parsing Excel data: %v", parseErr)
		gjm.UpdateJobStatus(jobID, "failed", 0, 0, &errorMsg, nil)
		return
	}

	totalElements := len(elementTypes)
	log.Printf("Processing %d element types from Excel file", totalElements)

	// Update total items in job
	err = gjm.UpdateJobTotalItems(jobID, totalElements)
	if err != nil {
		log.Printf("Error updating total items: %v", err)
	}

	// Process elements in batches with enhanced cancellation
	processedItems := 0
	successCount := 0
	errorCount := 0
	var errors []string

	// Create a ticker for periodic progress updates
	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()

	// Create a channel for progress updates
	progressChan := make(chan struct{})
	// Channel will be closed manually at the end

	// Start progress updater goroutine
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-progressChan:
				return
			case <-progressTicker.C:
				// Update progress in DB
				progress := (processedItems * 100) / totalElements
				if updateErr := gjm.UpdateJobStatus(jobID, "processing", progress, processedItems, nil, nil); updateErr != nil {
					log.Printf("Error updating job progress: %v", updateErr)
				}
			}
		}
	}()

	for i := 0; i < totalElements; i += batchSize {
		// Enhanced cancellation check before each batch
		select {
		case <-ctx.Done():
			log.Printf("Job %d cancelled during processing", jobID)
			errorMsg := "Job cancelled by user"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			// Reset global flags when job is cancelled
			gjm.ResetGlobalTerminationFlag()
			gjm.UnblockJobCreation()
			// Don't close progressChan here - let defer handle it
			return
		default:
			// Continue processing
		}

		// Additional context check
		if ctx.Err() != nil {
			log.Printf("Job %d context error detected: %v", jobID, ctx.Err())
			errorMsg := "Job cancelled due to context error"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// Check if context is done
		select {
		case <-ctx.Done():
			log.Printf("Job %d context cancelled during processing", jobID)
			errorMsg := "Job cancelled by context"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		default:
			// Continue processing
		}

		// Check if job was terminated (immediate check)
		if gjm.IsJobTerminated(jobID) {
			log.Printf("Job %d termination detected during processing", jobID)
			errorMsg := "Job terminated by user"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// Check if global termination is set (immediate check)
		if gjm.IsGlobalTerminationSet() {
			log.Printf("Job %d global termination detected during processing", jobID)
			errorMsg := "Job terminated by global flag"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// Check if job is still running (verification)
		if !gjm.IsJobRunning(jobID) {
			log.Printf("Job %d is not running, stopping processing", jobID)
			errorMsg := "Job stopped - not running"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// IMMEDIATE TERMINATION CHECKS - Check every iteration
		if gjm.IsJobTerminated(jobID) {
			log.Printf("🚨 IMMEDIATE TERMINATION: Job %d was terminated, stopping processing", jobID)
			errorMsg := "Job terminated"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		if gjm.IsGlobalTerminationSet() {
			log.Printf("🚨 IMMEDIATE TERMINATION: Global termination set - stopping processing for job %d", jobID)
			errorMsg := "Global termination"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		if !gjm.IsJobRunning(jobID) {
			log.Printf("🚨 IMMEDIATE TERMINATION: Job %d is not running, stopping processing", jobID)
			errorMsg := "Job stopped - not running"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// Check database status for immediate termination
		var dbJobStatus string
		if err := gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Select("status").Scan(&dbJobStatus).Error; err == nil {
			if dbJobStatus == "cancelled" || dbJobStatus == "terminated" {
				log.Printf("🚨 IMMEDIATE TERMINATION: Job %d status is %s in database, stopping processing", jobID, dbJobStatus)
				errorMsg := "Job terminated in database"
				gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
				return
			}
		}

		// Check if job was terminated (immediate check)
		if gjm.IsJobTerminated(jobID) {
			log.Printf("JOB LIFECYCLE: Job %d was terminated, stopping processing", jobID)
			errorMsg := "Job terminated"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// Check if global termination is set (immediate check)
		if gjm.IsGlobalTerminationSet() {
			log.Printf("JOB LIFECYCLE: Global termination set - stopping processing for job %d", jobID)
			errorMsg := "Global termination"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// Check for global termination
		if gjm.IsGlobalTerminationSet() {
			log.Printf("Job %d global termination detected during processing", jobID)
			errorMsg := "Job cancelled due to global termination"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			// Reset global flags when job is cancelled
			gjm.ResetGlobalTerminationFlag()
			gjm.UnblockJobCreation()
			return
		}

		// Check if job was terminated (immediate check)
		if gjm.IsJobTerminated(jobID) {
			log.Printf("Job %d was terminated, stopping processing", jobID)
			errorMsg := "Job terminated"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// Check if job was terminated (immediate check)
		if gjm.IsJobTerminated(jobID) {
			log.Printf("Job %d was terminated, stopping processing", jobID)
			errorMsg := "Job terminated"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// Check if job was terminated
		if gjm.IsJobTerminated(jobID) {
			log.Printf("Job %d termination detected during processing", jobID)
			errorMsg := "Job terminated"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			// Reset global flags when job is cancelled
			gjm.ResetGlobalTerminationFlag()
			gjm.UnblockJobCreation()
			return
		}

		// Check job status in database
		var jobStatus string
		if err := gjm.db.Model(&models.ImportJobGorm{}).Select("status").Where("id = ?", jobID).Scan(&jobStatus).Error; err == nil {
			if jobStatus == "cancelled" || jobStatus == "terminated" {
				log.Printf("Job %d database status is %s, stopping processing", jobID, jobStatus)
				errorMsg := "Job cancelled in database"
				gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
				return
			}
		}

		end := i + batchSize
		if end > totalElements {
			end = totalElements
		}

		batch := elementTypes[i:end]

		// 🚨 IMMEDIATE TERMINATION CHECKS - Before batch processing
		if gjm.IsJobTerminated(jobID) {
			log.Printf("🚨 IMMEDIATE TERMINATION: Job %d was terminated before batch processing", jobID)
			errorMsg := "Job terminated"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		if gjm.IsGlobalTerminationSet() {
			log.Printf("🚨 IMMEDIATE TERMINATION: Global termination set before batch processing for job %d", jobID)
			errorMsg := "Global termination"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// Check database status for immediate termination
		var batchDbJobStatus string
		if err := gjm.db.Model(&models.ImportJobGorm{}).Where("id = ?", jobID).Select("status").Scan(&batchDbJobStatus).Error; err == nil {
			if batchDbJobStatus == "cancelled" || batchDbJobStatus == "terminated" {
				log.Printf("🚨 IMMEDIATE TERMINATION: Job %d status is %s in database before batch processing", jobID, batchDbJobStatus)
				errorMsg := "Job terminated in database"
				gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
				return
			}
		}

		// Process batch with enhanced cancellation
		batchErrors := gjm.processBatchWithEnhancedCancellation(ctx, batch, jobID)

		// Check if batch was cancelled
		if len(batchErrors) > 0 && batchErrors[0] == "cancelled" {
			log.Printf("Job %d cancelled during batch processing", jobID)
			errorMsg := "Job cancelled by user"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		if len(batchErrors) > 0 {
			errors = append(errors, batchErrors...)
			errorCount += len(batchErrors)
		} else {
			successCount += len(batch)
		}

		processedItems += len(batch)
		progress := (processedItems * 100) / totalElements

		// Update progress
		updateErr := gjm.UpdateJobStatus(jobID, "processing", progress, processedItems, nil, nil)
		if updateErr != nil {
			log.Printf("Error updating job progress: %v", updateErr)
		}

		// Log batch progress
		log.Printf("Processed batch %d/%d: %d items, %d errors",
			(i/batchSize)+1, (totalElements+batchSize-1)/batchSize, len(batch), len(batchErrors))

		// Check for immediate termination after batch processing
		if gjm.IsJobTerminated(jobID) {
			log.Printf("Job %d was terminated after batch processing", jobID)
			errorMsg := "Job terminated"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		if gjm.IsGlobalTerminationSet() {
			log.Printf("Global termination set after batch processing for job %d", jobID)
			errorMsg := "Global termination"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		}

		// Additional cancellation check after each batch
		select {
		case <-ctx.Done():
			log.Printf("Job %d cancelled after batch processing", jobID)
			errorMsg := "Job cancelled by user"
			gjm.UpdateJobStatus(jobID, "terminated", 0, processedItems, &errorMsg, nil)
			return
		default:
			// Continue processing
		}
	}

	// Signal the progress updater to stop
	// Safe close - only close if not already closed
	select {
	case <-progressChan:
		// Channel already closed
	default:
		close(progressChan)
	}

	// Check if job was terminated before determining final status
	var finalStatus string
	var resultMsg string
	var errorMsg *string

	if gjm.IsJobTerminated(jobID) || gjm.IsGlobalTerminationSet() {
		log.Printf("Job %d was terminated, setting status to cancelled", jobID)
		finalStatus = "cancelled"
		errorMsgStr := "Job cancelled by user"
		errorMsg = &errorMsgStr
		resultMsg = fmt.Sprintf("Job cancelled - processed %d elements before termination", processedItems)
	} else {
		// Check job status in database
		var jobStatus string
		if err := gjm.db.Model(&models.ImportJobGorm{}).Select("status").Where("id = ?", jobID).Scan(&jobStatus).Error; err == nil {
			if jobStatus == "cancelled" || jobStatus == "terminated" {
				log.Printf("Job %d database status is %s, setting final status to cancelled", jobID, jobStatus)
				finalStatus = "cancelled"
				errorMsgStr := "Job cancelled in database"
				errorMsg = &errorMsgStr
				resultMsg = fmt.Sprintf("Job cancelled - processed %d elements before termination", processedItems)
			} else {
				// Determine final status and result
				if errorCount == 0 {
					finalStatus = "completed"
					resultMsg = fmt.Sprintf("Successfully processed all %d elements", successCount)
				} else if successCount == 0 {
					finalStatus = "failed"
					errorMsgStr := fmt.Sprintf("Failed to process any elements. Errors: %v", errors)
					errorMsg = &errorMsgStr
				} else {
					finalStatus = "completed_with_errors"
					errorDetails := strings.Join(errors, "\n")
					resultMsg = fmt.Sprintf("Processed %d elements successfully, %d errors occurred", successCount, errorCount)
					errorMsg = &errorDetails
				}
			}
		} else {
			// Determine final status and result
			if errorCount == 0 {
				finalStatus = "completed"
				resultMsg = fmt.Sprintf("Successfully processed all %d elements", successCount)
			} else if successCount == 0 {
				finalStatus = "failed"
				errorMsgStr := fmt.Sprintf("Failed to process any elements. Errors: %v", errors)
				errorMsg = &errorMsgStr
			} else {
				finalStatus = "completed_with_errors"
				errorDetails := strings.Join(errors, "\n")
				resultMsg = fmt.Sprintf("Processed %d elements successfully, %d errors occurred", successCount, errorCount)
				errorMsg = &errorDetails
			}
		}
	}

	// Calculate total time
	totalTime := time.Since(startTime)

	// Update final job status with timing information
	finalResultMsg := fmt.Sprintf("%s. Total time: %v", resultMsg, totalTime)
	gjm.UpdateJobStatus(jobID, finalStatus, 100, processedItems, errorMsg, &finalResultMsg)

	// Log completion with timing
	log.Printf("🚨 JOB COMPLETION: Job %d completed with status: %s, processed: %d, errors: %d, total time: %v",
		jobID, finalStatus, successCount, errorCount, totalTime)

	// Reset global termination flags if job completed successfully
	if finalStatus == "completed" {
		gjm.ResetGlobalTerminationFlag()
		gjm.UnblockJobCreation()
		log.Printf("🚨 JOB SUCCESS: Job %d completed successfully - resetting global flags", jobID)
	}
}

// createHierarchyName creates a properly formatted hierarchy name from header and sub-header
func createHierarchyName(header, subHeader string) string {
	// Clean both header and sub-header: replace spaces and hyphens with underscores
	cleanHeader := strings.ReplaceAll(strings.TrimSpace(header), " ", "_")
	cleanSubHeader := strings.ReplaceAll(strings.TrimSpace(subHeader), " ", "_")

	// Replace hyphens with underscores (ltree doesn't support hyphens)
	cleanHeader = strings.ReplaceAll(cleanHeader, "-", "_")
	cleanSubHeader = strings.ReplaceAll(cleanSubHeader, "-", "_")

	// Convert to lowercase for consistency
	cleanHeader = strings.ToLower(cleanHeader)
	cleanSubHeader = strings.ToLower(cleanSubHeader)

	// Handle common naming patterns
	// Convert "tower" to "tawor" if needed
	cleanHeader = strings.ReplaceAll(cleanHeader, "tower_", "tawor_")
	cleanSubHeader = strings.ReplaceAll(cleanSubHeader, "tower_", "tawor_")

	// Join with dot notation for ltree format
	var result string
	if cleanHeader != "" && cleanSubHeader != "" {
		result = cleanHeader + "." + cleanSubHeader
	} else if cleanSubHeader != "" {
		result = cleanSubHeader
	} else if cleanHeader != "" {
		result = cleanHeader
	} else {
		return ""
	}

	// Final sanitization to ensure ltree compatibility
	return sanitizeHierarchyNameForLtree(result)
}

// sanitizeHierarchyNameForLtree sanitizes hierarchy names for ltree queries
// ltree doesn't allow hyphens, so we replace them with underscores
func sanitizeHierarchyNameForLtree(name string) string {
	// Replace hyphens with underscores (ltree doesn't support hyphens)
	sanitized := strings.ReplaceAll(name, "-", "_")
	// Also ensure no spaces
	sanitized = strings.ReplaceAll(sanitized, " ", "_")
	return sanitized
}

// generateHierarchyVariations generates different variations of hierarchy names for flexible matching
func generateHierarchyVariations(name string) []string {
	variations := []string{}
	titleCaser := cases.Title(language.Und)

	// Original name
	variations = append(variations, name)

	// Try with different case combinations
	variations = append(variations, strings.ToUpper(name))
	variations = append(variations, titleCaser.String(name))

	// Try with different separator variations (but preserve dot notation for ltree)
	// Don't change underscores to dashes or remove them - keep the underscore format
	// variations = append(variations, strings.ReplaceAll(name, "_", "-"))
	// variations = append(variations, strings.ReplaceAll(name, "_", ""))

	// Try with different tower naming conventions (handle dot notation)
	variations = append(variations, strings.ReplaceAll(name, "tawor_", "tower_"))
	variations = append(variations, strings.ReplaceAll(name, "tawor_", "km_"))
	variations = append(variations, strings.ReplaceAll(name, "tower_", "km_"))
	variations = append(variations, strings.ReplaceAll(name, "km_", "tower_"))
	variations = append(variations, strings.ReplaceAll(name, "km_", "tawor_"))

	// Try with different floor naming conventions (handle dot notation)
	variations = append(variations, strings.ReplaceAll(name, "floor_", "kmfloor_"))
	variations = append(variations, strings.ReplaceAll(name, "kmfloor_", "floor_"))
	variations = append(variations, strings.ReplaceAll(name, ".floor_", ".kmfloor_"))
	variations = append(variations, strings.ReplaceAll(name, ".kmfloor_", ".floor_"))

	// Add specific KM variations to help with matching
	variations = append(variations, strings.ReplaceAll(name, "km_", "tower_"))
	variations = append(variations, strings.ReplaceAll(name, "tower_", "km_"))

	// Remove duplicates
	seen := make(map[string]bool)
	uniqueVariations := []string{}
	for _, v := range variations {
		if !seen[v] {
			seen[v] = true
			uniqueVariations = append(uniqueVariations, v)
		}
	}

	return uniqueVariations
}
