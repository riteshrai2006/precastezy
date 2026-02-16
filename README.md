# Precast Management System - Job System

## Overview

The system now includes a job-based import system to handle large Excel/CSV imports without gateway timeouts. Instead of processing files synchronously, the system creates background jobs that can be monitored for progress.

## Job System Features

### Job States
- `pending`: Job is created and waiting to start
- `processing`: Job is currently running
- `completed`: Job finished successfully
- `failed`: Job encountered an error

### Job Progress Tracking
- Progress percentage (0-100)
- Total items to process
- Processed items count
- Error messages (if any)
- Result summary

## API Endpoints

### Create Import Job
```
POST /api/project/{project_id}/jobs?job_type=element_type_import
```
Creates a new import job and returns the job ID immediately.

**Response:**
```json
{
  "message": "Import job created successfully",
  "job_id": 123,
  "status": "pending"
}
```

### Check Job Status
```
GET /api/jobs/{job_id}
```
Returns the current status and progress of a job.

**Response:**
```json
{
  "job": {
    "id": 123,
    "project_id": 1,
    "job_type": "element_type_import",
    "status": "processing",
    "progress": 45,
    "total_items": 100,
    "processed_items": 45,
    "created_by": "admin",
    "created_at": "2024-01-01T10:00:00Z",
    "updated_at": "2024-01-01T10:05:00Z",
    "completed_at": null,
    "error": null,
    "result": null
  }
}
```

### Get All Jobs for Project
```
GET /api/project/{project_id}/jobs
```
Returns all jobs for a specific project.

**Response:**
```json
{
  "jobs": [
    {
      "id": 123,
      "project_id": 1,
      "job_type": "element_type_import",
      "status": "completed",
      "progress": 100,
      "total_items": 100,
      "processed_items": 100,
      "created_by": "admin",
      "created_at": "2024-01-01T10:00:00Z",
      "updated_at": "2024-01-01T10:10:00Z",
      "completed_at": "2024-01-01T10:10:00Z",
      "error": null,
      "result": "Successfully processed 100 elements"
    }
  ],
  "count": 1
}
```

## Modified Import Endpoints

### Excel Import
```
POST /api/project/{project_id}/import/excel
```
Now returns a job ID immediately and processes the file in the background.

**Response:**
```json
{
  "message": "Import job started successfully",
  "job_id": 123,
  "status": "pending",
  "total_elements": 50,
  "batch_size": 30,
  "concurrent_batches": 15
}
```

### CSV Import
```
POST /api/project/{project_id}/import_csv_element_type
```
Now returns a job ID immediately and processes the file in the background.

## Usage Flow

1. **Start Import**: Call the import endpoint with your Excel/CSV file
2. **Get Job ID**: The response includes a `job_id`
3. **Monitor Progress**: Poll the job status endpoint to check progress
4. **Check Results**: Once completed, check the result field for summary

## Example Usage

```javascript
// Start import
const response = await fetch('/api/project/1/import/excel', {
  method: 'POST',
  body: formData
});
const { job_id } = await response.json();

// Monitor progress
const checkStatus = async () => {
  const statusResponse = await fetch(`/api/jobs/${job_id}`);
  const { job } = await statusResponse.json();
  
  if (job.status === 'completed') {
    console.log('Import completed:', job.result);
  } else if (job.status === 'failed') {
    console.error('Import failed:', job.error);
  } else {
    console.log(`Progress: ${job.progress}%`);
    setTimeout(checkStatus, 2000); // Check again in 2 seconds
  }
};

checkStatus();
```

## Database Schema

The job system uses a new `import_jobs` table:

```sql
CREATE TABLE import_jobs (
    id SERIAL PRIMARY KEY,
    project_id INTEGER NOT NULL,
    job_type VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    progress INTEGER NOT NULL DEFAULT 0,
    total_items INTEGER NOT NULL DEFAULT 0,
    processed_items INTEGER NOT NULL DEFAULT 0,
    created_by VARCHAR(100) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMP WITH TIME ZONE,
    error TEXT,
    result TEXT
);
```

## Benefits

1. **No Gateway Timeouts**: Large imports no longer cause timeout errors
2. **Progress Tracking**: Real-time progress updates
3. **Error Handling**: Detailed error messages for failed imports
4. **Background Processing**: Server can handle other requests while importing
5. **Scalability**: Can handle multiple concurrent imports
6. **Monitoring**: Easy to track and debug import issues 

 