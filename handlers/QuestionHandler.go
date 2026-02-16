package handlers

import (
	"backend/models"
	"backend/storage"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"backend/services"

	"github.com/gin-gonic/gin"
)

// CreateQuestions godoc
// @Summary      Create questions (paper with questions)
// @Tags         questions
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Questions payload"
// @Success      200   {object}  object
// @Failure      400   {object}  object
// @Failure      401   {object}  object
// @Router       /api/questions [post]
func CreateQuestions(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Define request structure with an array of questions and a single paper_name
		var req struct {
			PaperName string `json:"paper_name"` // Paper name for the set
			ProjectID int    `json:"project_id"` // Project ID for the paper
			Questions []struct {
				QuestionText string   `json:"question_text"`
				Options      []string `json:"options"`
			} `json:"questions"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Begin a transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}

		// Check if the paper already exists, otherwise create a new one
		var paperID int
		err = tx.QueryRow(`SELECT id FROM papers WHERE name = $1 AND project_id = $2`, req.PaperName, req.ProjectID).Scan(&paperID)
		if err != nil {
			// If no paper exists with the given name and project_id, create a new paper
			err = tx.QueryRow(`INSERT INTO papers (name, project_id) VALUES ($1, $2) RETURNING id`, req.PaperName, req.ProjectID).Scan(&paperID)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create paper"})
				return
			}
		}

		// Insert the questions for the given paper_id and project_id
		for _, question := range req.Questions {
			// Insert the question with the generated paper_id and project_id
			var questionID int
			err = tx.QueryRow(`INSERT INTO questions (question_text, paper_id, project_id, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
				question.QuestionText, paperID, req.ProjectID, time.Now()).Scan(&questionID)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert question", "details": err.Error()})
				return
			}

			// Insert options for the question
			for _, option := range question.Options {
				_, err = tx.Exec(`INSERT INTO options (question_id, option_text) VALUES ($1, $2)`, questionID, option)
				if err != nil {
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert options"})
					return
				}
			}
		}

		// Commit the transaction
		tx.Commit()
		c.JSON(http.StatusOK, gin.H{"message": "Questions created successfully", "paper_id": paperID, "project_id": req.ProjectID})

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", req.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", req.ProjectID)
		}

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who created the questions
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("Questions created for paper: %s in project: %s", req.PaperName, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", req.ProjectID),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.Exec(`
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, req.ProjectID,
			fmt.Sprintf("Questions created for paper: %s in project: %s", req.PaperName, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", req.ProjectID))

		log := models.ActivityLog{
			EventContext: "Questions",
			EventName:    "Create",
			Description:  "Create questions of" + req.PaperName,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    req.ProjectID,
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetQuestions godoc
// @Summary      Get questions by paper ID
// @Tags         questions
// @Param        paper_id  path  int  true  "Paper ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/questions/{paper_id} [get]
func GetQuestions(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Get paper_id from the URL parameter
		paperIDParam := c.Param("paper_id")
		paperID, err := strconv.Atoi(paperIDParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid paper ID"})
			return
		}

		// Query to fetch the paper details
		var paper models.Paper
		err = db.QueryRow(`SELECT id, name, project_id FROM papers WHERE id = $1`, paperID).Scan(&paper.ID, &paper.Name, &paper.ProjectID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Paper not found"})
			return
		}

		// Query to fetch the questions for the paper
		rows, err := db.Query(`SELECT id, question_text, paper_id, project_id, created_at FROM questions WHERE paper_id = $1`, paperID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch questions"})
			return
		}
		defer rows.Close()

		var questions []models.Question
		for rows.Next() {
			var question models.Question
			if err := rows.Scan(&question.ID, &question.QuestionText, &question.PaperID, &question.ProjectID, &question.CreatedAt); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan question"})
				return
			}

			// Query to fetch options for each question
			optionRows, err := db.Query(`SELECT id, question_id, option_text FROM options WHERE question_id = $1`, question.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch options"})
				return
			}

			var options []models.Option
			for optionRows.Next() {
				var option models.Option
				if err := optionRows.Scan(&option.ID, &option.QuestionID, &option.OptionText); err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan option"})
					return
				}
				options = append(options, option)
			}
			optionRows.Close()

			// Add options to the question
			question.Options = options
			questions = append(questions, question)
		}

		// Return the paper along with its questions and options
		c.JSON(http.StatusOK, gin.H{
			"paper":     paper,
			"questions": questions,
		})

		log := models.ActivityLog{
			EventContext: "Questions",
			EventName:    "Get",
			Description:  "Get questions of" + paper.Name,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    paper.ProjectID,
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// SubmitAnswers godoc
// @Summary      Submit answers for questions
// @Tags         questions
// @Accept       json
// @Produce      json
// @Param        body  body  object  true  "Answers payload"
// @Success      200   {object}  object
// @Failure      400   {object}  object
// @Failure      401   {object}  object
// @Router       /api/questions/answers [post]
func SubmitAnswers(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Start transaction

		log.Printf("starting transaction")
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin transaction", "details": err.Error()})
			return
		}
		committed := false
		defer func() {
			if !committed {
				tx.Rollback()
			}
		}()

		log.Printf("1")

		var userID int
		if err = tx.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&userID); err != nil {
			handleDBError(c, "session", err)
			return
		}
		log.Printf("3")

		var requestBody struct {
			Answers []struct {
				ProjectID  int    `json:"project_id"`
				QuestionID int    `json:"question_id"`
				OptionID   int    `json:"option_id"`
				TaskID     int    `json:"task_id"`
				StageID    int    `json:"stage_id"`
				Comment    string `json:"comment"`
				ImagePath  string `json:"image_path"`
			} `json:"answers"`
			Status struct {
				ActivityID int    `json:"activity_id"`
				Status     string `json:"status"`
			} `json:"status"`
		}

		log.Printf("4")

		if err = c.ShouldBindJSON(&requestBody); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "details": err.Error()})
			return
		}

		log.Printf("5")

		log.Printf("Received %d answers from user %d", len(requestBody.Answers), userID)
		log.Printf("Status update: ActivityID=%d, Status=%s", requestBody.Status.ActivityID, requestBody.Status.Status)

		// Get activity and stage details first to get element_id
		activity, currStage, err := getActivityDetails(tx, requestBody.Status.ActivityID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"details": "error in get ActivityDetails", "error": err.Error()})
			return
		}

		// Insert answers with element_id from activity
		if err = insertAnswers(tx, userID, requestBody.Answers, activity.ElementID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"details": "error in get InsertAnswers", "error": err.Error()})
			return
		}

		log.Printf("6")

		// Removed impossible condition: nil != nil

		log.Printf("7")

		// Get QC IDs for special stages
		meshMoldQC, reinforcementQC, reinforcementID, err := getQCInfo(tx, activity.ProjectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"details": "error in get getQcInfo", "error": err.Error()})
			return
		}

		log.Printf("8")

		// Update activity status based on current stage
		if err = updateActivityStatus(tx, currStage, userID, meshMoldQC, reinforcementQC, activity, requestBody.Status.Status); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"details": "error in get UpdateActivityStatus", "error": err.Error()})
			return
		}

		log.Printf("9")

		// Handle stage transitions
		if strings.EqualFold(requestBody.Status.Status, "completed") { // , userID, requestBody.Status.Status
			if err = handleStageTransition(tx, currStage, activity, reinforcementID, requestBody.Status.Status); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		}

		log.Printf("10")

		// Update complete production
		if err = updateCompleteProduction(tx, requestBody.Status.ActivityID, userID, requestBody.Status.Status); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"details": "error in get UpdateCompleteProduction", "error": err.Error()})
			return
		}

		log.Printf("11")

		if err = tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}
		committed = true

		log.Printf("12")

		// Send notification to next assignee if status is "completed"
		if strings.EqualFold(requestBody.Status.Status, "completed") {
			// Get next stage ID using the same logic as getNextStageID
			var elementTypeID int
			err = db.QueryRow(`SELECT element_type_id FROM task WHERE task_id = $1`, activity.TaskID).Scan(&elementTypeID)
			if err == nil {
				var stagePath string
				err = db.QueryRow(`SELECT stage_path FROM element_type_path WHERE element_type_id = $1`, elementTypeID).Scan(&stagePath)
				if err == nil {
					// Convert stage path to array
					stagePath = strings.Trim(stagePath, "{}")
					stagePathArray := strings.Split(stagePath, ",")
					var nextStageID int
					for i, stage := range stagePathArray {
						stageInt, _ := strconv.Atoi(strings.TrimSpace(stage))
						if stageInt == activity.StageID && i+1 < len(stagePathArray) {
							nextStage, _ := strconv.Atoi(strings.TrimSpace(stagePathArray[i+1]))
							nextStageID = nextStage
							break
						}
					}

					if nextStageID > 0 {
						// Get next assignee from next stage
						var nextAssigneeID sql.NullInt64
						err = db.QueryRow(`
							SELECT assigned_to FROM project_stages WHERE id = $1 LIMIT 1
						`, nextStageID).Scan(&nextAssigneeID)
						if err == nil && nextAssigneeID.Valid {
							// Get project name for notification
							var projectName string
							err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", activity.ProjectID).Scan(&projectName)
							if err != nil {
								log.Printf("Failed to fetch project name: %v", err)
								projectName = fmt.Sprintf("Project %d", activity.ProjectID)
							}

							// Send notification to next assignee
							notif := models.Notification{
								UserID:    int(nextAssigneeID.Int64),
								Message:   fmt.Sprintf("Task assigned to you in project: %s", projectName),
								Status:    "unread",
								Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", activity.ProjectID),
								CreatedAt: time.Now(),
								UpdatedAt: time.Now(),
							}

							_, err = db.Exec(`
								INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
								VALUES ($1, $2, $3, $4, $5, $6)
							`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

							if err != nil {
								log.Printf("Failed to insert notification: %v", err)
							}

							var taskName string
							err = db.QueryRow("SELECT name FROM task WHERE task_id = $1", activity.TaskID).Scan(&taskName)
							if err != nil {
								log.Printf("Failed to fetch task name: %v", err)
								taskName = fmt.Sprintf("Task %d", activity.TaskID)
							}

							// Get activity name for better context
							var activityName string
							err = db.QueryRow("SELECT name FROM activity WHERE id = $1", requestBody.Status.ActivityID).Scan(&activityName)
							if err != nil {
								log.Printf("Failed to fetch activity name: %v", err)
								activityName = fmt.Sprintf("Activity %d", requestBody.Status.ActivityID)
							}

							// Send push notification to the next assignee
							log.Printf("Attempting to send push notification to next assignee %d for task assignment", int(nextAssigneeID.Int64))
							SendNotificationHelper(db, int(nextAssigneeID.Int64),
								"Task Assigned",
								fmt.Sprintf("New task assigned in project '%s': %s", projectName, taskName),
								map[string]string{
									"project_id":    strconv.Itoa(activity.ProjectID),
									"project_name":  projectName,
									"task_id":       strconv.Itoa(activity.TaskID),
									"task_name":     taskName,
									"activity_id":   strconv.Itoa(requestBody.Status.ActivityID),
									"activity_name": activityName,
									"action":        "task_assigned",
								},
								"task_assigned")
						}
					}
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"message": "Answers submitted successfully and status updated"})

		log := models.ActivityLog{
			EventContext: "Answers",
			EventName:    "Submit",
			Description:  "Submit Answers",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    0,
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// Helper functions

func handleDBError(c *gin.Context, resource string, err error) {
	log.Printf("13")
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("%s not found", resource)})
	} else {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to fetch %s details", resource)})
	}
}

func insertAnswers(tx *sql.Tx, userID int, answers []struct {
	ProjectID  int    `json:"project_id"`
	QuestionID int    `json:"question_id"`
	OptionID   int    `json:"option_id"`
	TaskID     int    `json:"task_id"`
	StageID    int    `json:"stage_id"`
	Comment    string `json:"comment"`
	ImagePath  string `json:"image_path"`
}, elementID int) error {
	stmt, err := tx.Prepare(`
        INSERT INTO qc_answers (
            qc_id, project_id, question_id, option_id, task_id, stage_id, 
            comment, image_path, created_at, element_id
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
    `)
	if err != nil {
		return fmt.Errorf("failed to prepare insert query: %v", err)
	}
	defer stmt.Close()

	log.Printf("14")
	for _, answer := range answers {
		log.Printf("Inserting answer: %+v", answer)
		log.Printf("15")

		if _, err = stmt.Exec(
			userID, answer.ProjectID, answer.QuestionID, answer.OptionID,
			answer.TaskID, answer.StageID, answer.Comment, answer.ImagePath, time.Now(), elementID,
		); err != nil {
			log.Printf("Failed to insert answer: %+v, error: %v", answer, err)
			return fmt.Errorf("failed to insert answer: %v", err)
		}
	}

	return nil
}

func getActivityDetails(tx *sql.Tx, activityID int) (activity struct {
	ID                    int
	TaskID                int
	QCID                  sql.NullInt64
	StageID               int
	ProjectID             int
	Status                string
	MeshMoldStatus        string
	ReinforcementStatus   string
	MeshMoldQCStatus      string
	ReinforcementQCStatus string
	ElementID             int
	StockyardID           int
}, currStage string, err error) {
	err = tx.QueryRow(`
        SELECT 
            id, task_id, qc_id, stage_id, project_id, status,
            mesh_mold_status, reinforcement_status,
            meshmold_qc_status, reinforcement_qc_status,
            element_id, stockyard_id
        FROM activity WHERE id = $1
    `, activityID).Scan(
		&activity.ID,
		&activity.TaskID,
		&activity.QCID,
		&activity.StageID,
		&activity.ProjectID,
		&activity.Status,
		&activity.MeshMoldStatus,
		&activity.ReinforcementStatus,
		&activity.MeshMoldQCStatus,
		&activity.ReinforcementQCStatus,
		&activity.ElementID,
		&activity.StockyardID,
	)
	if err != nil {
		return activity, "", fmt.Errorf("failed to fetch activity details: %v", err)
	}

	log.Printf("16")

	err = tx.QueryRow(`
        SELECT name FROM project_stages 
        WHERE id = $1 AND project_id = $2
    `, activity.StageID, activity.ProjectID).Scan(&currStage)
	if err != nil {
		return activity, "", fmt.Errorf("failed to fetch current stage: %v", err)
	}

	log.Printf("17")

	return activity, currStage, nil
}

func getQCInfo(tx *sql.Tx, projectID int) (meshMoldQC, reinforcementQC, reinforcementID int, err error) {
	err = tx.QueryRow(`
        SELECT qc_id FROM project_stages 
        WHERE name = 'Mesh & Mould' AND project_id = $1
    `, projectID).Scan(&meshMoldQC)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to fetch Mesh & Mould QC: %v", err)
	}

	log.Printf("18")

	err = tx.QueryRow(`
        SELECT qc_id, id FROM project_stages 
        WHERE name = 'Reinforcement' AND project_id = $1
    `, projectID).Scan(&reinforcementQC, &reinforcementID)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to fetch Reinforcement QC: %v", err)
	}

	log.Printf("19")

	return meshMoldQC, reinforcementQC, reinforcementID, nil
}

func updateActivityStatus(tx *sql.Tx, currStage string, userID, meshMoldQC, reinforcementQC int, activity struct {
	ID                    int
	TaskID                int
	QCID                  sql.NullInt64
	StageID               int
	ProjectID             int
	Status                string
	MeshMoldStatus        string
	ReinforcementStatus   string
	MeshMoldQCStatus      string
	ReinforcementQCStatus string
	ElementID             int
	StockyardID           int
}, status string) error {
	currStageLower := strings.ToLower(currStage)
	statusLower := strings.ToLower(status)

	log.Printf("20")

	switch {
	case currStageLower == "mesh & mould" && userID == meshMoldQC && strings.ToLower(activity.MeshMoldStatus) == "completed":
		_, err := tx.Exec(`
            UPDATE activity SET meshmold_qc_status = $1 
            WHERE id = $2
        `, status, activity.ID)
		if err != nil {
			return fmt.Errorf("failed to update Mesh & Mould QC status: %v", err)
		}

		var ElementTypeID, FloorID sql.NullInt64
		err = tx.QueryRow(`
				SELECT element_type_id, floor_id 
				FROM task WHERE task_id = $1 LIMIT 1
			`, activity.TaskID).Scan(&ElementTypeID, &FloorID)
		if err != nil {
			return fmt.Errorf("failed to fetch element type and floor ID: %v", err)
		}

		queryInsertCompleteProduction := `
				INSERT INTO complete_production (
				task_id, activity_id, project_id, element_id, element_type_id, floor_id, 
				stage_id, user_id, started_at, updated_at, status
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				`

		_, err = tx.Exec(queryInsertCompleteProduction,
			activity.TaskID,
			activity.ID,
			activity.ProjectID,
			activity.ElementID,
			ElementTypeID,
			FloorID,
			activity.StageID,
			activity.QCID.Int64,
			time.Now(),
			time.Now(),
			status,
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert into complete_production: %v", err)
		}

	case currStageLower == "reinforcement" && userID == reinforcementQC && strings.ToLower(activity.ReinforcementStatus) == "completed":
		_, err := tx.Exec(`
            UPDATE activity SET reinforcement_qc_status = $1 
            WHERE id = $2
        `, status, activity.ID)
		if err != nil {
			return fmt.Errorf("failed to update Reinforcement QC status: %v", err)
		}

		var ElementTypeID, FloorID sql.NullInt64
		err = tx.QueryRow(`
				SELECT element_type_id, floor_id 
				FROM task WHERE task_id = $1 LIMIT 1
			`, activity.TaskID).Scan(&ElementTypeID, &FloorID)
		if err != nil {
			return fmt.Errorf("failed to fetch element type and floor ID: %v", err)
		}

		queryInsertCompleteProduction := `
				INSERT INTO complete_production (
				task_id, activity_id, project_id, element_id, element_type_id, floor_id, 
				stage_id, user_id, started_at, updated_at, status
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				`

		_, err = tx.Exec(queryInsertCompleteProduction,
			activity.TaskID,
			activity.ID,
			activity.ProjectID,
			activity.ElementID,
			ElementTypeID,
			FloorID,
			activity.StageID,
			activity.QCID.Int64,
			time.Now(),
			time.Now(),
			status,
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert into complete_production: %v", err)
		}

	case currStageLower != "mesh & mould" && currStageLower != "reinforcement":
		_, err := tx.Exec(`
            UPDATE activity SET qc_status = $1 
            WHERE id = $2
        `, status, activity.ID)
		if err != nil {
			return fmt.Errorf("failed to update activity QC status: %v", err)
		}

		var ElementTypeID, FloorID sql.NullInt64
		err = tx.QueryRow(`
				SELECT element_type_id, floor_id 
				FROM task WHERE task_id = $1 LIMIT 1
			`, activity.TaskID).Scan(&ElementTypeID, &FloorID)
		if err != nil {
			return fmt.Errorf("failed to fetch element type and floor ID: %v", err)
		}

		queryInsertCompleteProduction := `
				INSERT INTO complete_production (
				task_id, activity_id, project_id, element_id, element_type_id, floor_id, 
				stage_id, user_id, started_at, updated_at, status
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				`

		_, err = tx.Exec(queryInsertCompleteProduction,
			activity.TaskID,
			activity.ID,
			activity.ProjectID,
			activity.ElementID,
			ElementTypeID,
			FloorID,
			activity.StageID,
			activity.QCID.Int64,
			time.Now(),
			time.Now(),
			status,
		)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert into complete_production: %v", err)
		}

		if statusLower == "completed" {
			if err := recordCompletion(tx, activity); err != nil {
				return fmt.Errorf("failed to record completion: %v", err)
			}
		}
	}

	log.Printf("21")

	return nil
}

func recordCompletion(tx *sql.Tx, activity struct {
	ID                    int
	TaskID                int
	QCID                  sql.NullInt64
	StageID               int
	ProjectID             int
	Status                string
	MeshMoldStatus        string
	ReinforcementStatus   string
	MeshMoldQCStatus      string
	ReinforcementQCStatus string
	ElementID             int
	StockyardID           int
}) error {
	var elementTypeID, floorID sql.NullInt64
	err := tx.QueryRow(`
        SELECT element_type_id, floor_id 
        FROM task WHERE task_id = $1 LIMIT 1
    `, activity.TaskID).Scan(&elementTypeID, &floorID)
	if err != nil {
		return fmt.Errorf("failed to fetch element type and floor ID: %v", err)
	}

	log.Printf("22")

	return nil
}

func handleStageTransition(tx *sql.Tx, currStage string, activity struct {
	ID                    int
	TaskID                int
	QCID                  sql.NullInt64
	StageID               int
	ProjectID             int
	Status                string
	MeshMoldStatus        string
	ReinforcementStatus   string
	MeshMoldQCStatus      string
	ReinforcementQCStatus string
	ElementID             int
	StockyardID           int // , userID int, status string
}, reinforcementID int, status string) error {
	currStageLower := strings.ToLower(currStage)
	// statusLower := strings.ToLower(status)

	log.Printf("24")

	switch {
	case currStageLower != "mesh & mould" && currStageLower != "reinforcement":
		return handleRegularStageTransition(tx, activity)

	case currStageLower == "mesh & mould":
		return handleMeshMoldTransition(tx, activity, reinforcementID, status)

	case currStageLower == "reinforcement":
		return handleReinforcementTransition(tx, activity, status)
	}

	log.Printf("25")

	return nil
}

func handleRegularStageTransition(tx *sql.Tx, activity struct {
	ID                    int
	TaskID                int
	QCID                  sql.NullInt64
	StageID               int
	ProjectID             int
	Status                string
	MeshMoldStatus        string
	ReinforcementStatus   string
	MeshMoldQCStatus      string
	ReinforcementQCStatus string
	ElementID             int
	StockyardID           int
}) error {

	nextStageID, err := getNextStageID(tx, activity.StageID, activity.TaskID)
	if err != nil {
		return fmt.Errorf("failed to determine next stage: %v", err)
	}

	log.Printf("26")

	db := storage.GetDB()

	if nextStageID == 0 {
		if _, err = CreatePrecastStock(db, activity.ElementID, activity.ProjectID, activity.StockyardID); err != nil {
			return fmt.Errorf("failed to move to stockyard: %v", err)
		}

		if _, err = tx.Exec("UPDATE activity SET completed = true WHERE id = $1", activity.ID); err != nil {
			return fmt.Errorf("failed to remove activity: %v", err)
		}
		return nil
	}

	log.Printf("27")

	return moveToNextStageWithDetails(tx, activity, nextStageID)

}

func handleMeshMoldTransition(tx *sql.Tx, activity struct {
	ID                    int
	TaskID                int
	QCID                  sql.NullInt64
	StageID               int
	ProjectID             int
	Status                string
	MeshMoldStatus        string
	ReinforcementStatus   string
	MeshMoldQCStatus      string
	ReinforcementQCStatus string
	ElementID             int
	StockyardID           int
}, reinforcementID int, status string) error {

	log.Printf("Handling Mesh & Mould transition for Activity ID: %d, Task ID: %d, Stage ID: %d", activity.ID, activity.TaskID, activity.StageID)
	nextStageID, err := getNextStageID(tx, activity.StageID, activity.TaskID)
	if err != nil {
		return fmt.Errorf("failed to determine next stage: %v", err)
	}

	log.Printf("28")

	log.Printf("Next Stage ID: %d", nextStageID)

	meshComplete := strings.ToLower(activity.MeshMoldStatus) == "completed" &&
		(strings.ToLower(activity.MeshMoldQCStatus) == "completed" || strings.ToLower(status) == "completed")
	reinforcementComplete := strings.ToLower(activity.ReinforcementStatus) == "completed" &&
		strings.ToLower(activity.ReinforcementQCStatus) == "completed"

	if meshComplete {
		if reinforcementComplete {
			log.Printf("Both Mesh & Mould and Reinforcement are completed.")
			return moveToNextStageAfterSpecialStages(tx, activity, reinforcementID)
		}
		log.Printf("Only Mesh & Mould is completed.")
		return moveToNextStageWithDetails(tx, activity, nextStageID)
	}

	log.Printf("29")
	return nil
}

func handleReinforcementTransition(tx *sql.Tx, activity struct {
	ID                    int
	TaskID                int
	QCID                  sql.NullInt64
	StageID               int
	ProjectID             int
	Status                string
	MeshMoldStatus        string
	ReinforcementStatus   string
	MeshMoldQCStatus      string
	ReinforcementQCStatus string
	ElementID             int
	StockyardID           int
}, status string) error {

	log.Printf("30")

	log.Printf("Handling Reinforcement transition for Activity ID: %d, Task ID: %d, Stage ID: %d", activity.ID, activity.TaskID, activity.StageID)

	nextStageID, err := getNextStageID(tx, activity.StageID, activity.TaskID)
	if err != nil {
		return fmt.Errorf("failed to determine next stage: %v", err)
	}
	log.Printf("Next Stage ID: %d", nextStageID)

	meshComplete := strings.ToLower(activity.MeshMoldStatus) == "completed" &&
		strings.ToLower(activity.MeshMoldQCStatus) == "completed"

	reinforcementComplete := strings.ToLower(activity.ReinforcementStatus) == "completed" &&
		(strings.ToLower(activity.ReinforcementQCStatus) == "completed" || strings.ToLower(status) == "completed")

	if reinforcementComplete && meshComplete {
		return moveToNextStageWithDetails(tx, activity, nextStageID)
	}

	log.Printf("31")

	return nil
}

func moveToNextStageAfterSpecialStages(tx *sql.Tx, activity struct {
	ID                    int
	TaskID                int
	QCID                  sql.NullInt64
	StageID               int
	ProjectID             int
	Status                string
	MeshMoldStatus        string
	ReinforcementStatus   string
	MeshMoldQCStatus      string
	ReinforcementQCStatus string
	ElementID             int
	StockyardID           int
}, nextStageID int) error {

	log.Printf("32")

	nextStageId, err := getNextStageID(tx, activity.StageID, nextStageID)
	if err != nil {
		return fmt.Errorf("failed to determine next stage: %v", err)
	}

	var newAssignedTo, newQCID, newPaperID sql.NullInt64
	err = tx.QueryRow(`
        SELECT assigned_to, qc_id, paper_id
        FROM project_stages WHERE id = $1 LIMIT 1
    `, nextStageId).Scan(&newAssignedTo, &newQCID, &newPaperID)
	if err != nil {
		return fmt.Errorf("failed to fetch next stage details: %v", err)
	}

	log.Printf("33")

	var elementTypeID, floorID sql.NullInt64
	err = tx.QueryRow(`
        SELECT element_type_id, floor_id
        FROM task WHERE task_id = $1 LIMIT 1
    `, activity.TaskID).Scan(&elementTypeID, &floorID)
	if err != nil {
		return fmt.Errorf("failed to fetch element details: %v", err)
	}

	log.Printf("34")

	return moveToNextStage(tx, activity.ID, nextStageId)
}

func moveToNextStageWithDetails(tx *sql.Tx, activity struct {
	ID                    int
	TaskID                int
	QCID                  sql.NullInt64
	StageID               int
	ProjectID             int
	Status                string
	MeshMoldStatus        string
	ReinforcementStatus   string
	MeshMoldQCStatus      string
	ReinforcementQCStatus string
	ElementID             int
	StockyardID           int
}, nextStageID int) error {

	log.Printf("36")
	log.Printf("Both Reinforcement and Mesh & Mould are completed.")
	log.Printf("Next Stage ID: %d", nextStageID)

	// Fetch next stage details
	var newAssignedTo, newQCID, newPaperID sql.NullInt64
	err := tx.QueryRow(`
        SELECT assigned_to, qc_id, paper_id 
        FROM project_stages WHERE id = $1 LIMIT 1
    `, nextStageID).Scan(&newAssignedTo, &newQCID, &newPaperID)
	if err != nil {
		return fmt.Errorf("failed to fetch next stage details: %v", err)
	}

	log.Printf("New Assigned To: %v, New QC ID: %v, New Paper ID: %v", newAssignedTo, newQCID, newPaperID)

	// Fetch element details
	var elementTypeID, floorID sql.NullInt64
	err = tx.QueryRow(`
        SELECT element_type_id, floor_id 
        FROM task WHERE task_id = $1 LIMIT 1
    `, activity.TaskID).Scan(&elementTypeID, &floorID)
	if err != nil {
		return fmt.Errorf("failed to fetch element details: %v", err)
	}

	log.Printf("Moving to next stage with ID: %d", nextStageID)

	// Move to next stage
	if err := moveToNextStage(tx, activity.ID, nextStageID); err != nil {
		return fmt.Errorf("failed to move to next stage: %v", err)
	}

	// Fetch project name
	var projectName string
	err = tx.QueryRow(`SELECT name FROM project WHERE project_id = $1`, activity.ProjectID).Scan(&projectName)
	if err != nil {
		return fmt.Errorf("failed to fetch project name: %v", err)
	}

	// Fetch next assignee details
	if !newAssignedTo.Valid {
		return fmt.Errorf("next stage has no assigned_to user")
	}
	var firstName, lastName, email string
	err = tx.QueryRow(`SELECT first_name, last_name, email FROM users WHERE id = $1`, newAssignedTo.Int64).
		Scan(&firstName, &lastName, &email)
	if err != nil {
		return fmt.Errorf("failed to fetch next assignee details: %v", err)
	}

	// Fetch QC user details
	if !activity.QCID.Valid {
		return fmt.Errorf("QCID is NULL, cannot fetch QC user")
	}
	var qcFirstName, qcLastName string
	err = tx.QueryRow(`SELECT first_name, last_name FROM users WHERE id = $1`, activity.QCID.Int64).
		Scan(&qcFirstName, &qcLastName)
	if err != nil {
		return fmt.Errorf("failed to fetch QC user details: %v", err)
	}
	userName := qcFirstName + " " + qcLastName

	// Init email service (⚠️ consider injecting db instead of re-opening inside)
	db := storage.InitDB()
	//defer db.Close()
	emailService := services.NewEmailService(db)

	var clientID int
	err = db.QueryRow(`SELECT client_id FROM project WHERE project_id = $1`, activity.ProjectID).Scan(&clientID)
	if err != nil {
		fmt.Println("Error in finding the clientID:", err)
	}

	var organization string
	err = db.QueryRow(`SELECT organization FROM client WHERE client_id = $1`, clientID).Scan(&organization)
	if err != nil {
		fmt.Println("Error in finding the organization:", err)
	}

	// Build email data
	emailData := models.EmailData{
		Email:        email,
		Role:         "Project Member",
		Organization: "",
		ProjectName:  projectName,
		ProjectID:    strconv.Itoa(activity.ProjectID),
		CompanyName:  organization,
		SupportEmail: "support@blueinvent.com",
		LoginURL:     "https://precastezy.blueinvent.com/login",
		AdminName:    userName,                   // QC user
		UserName:     firstName + " " + lastName, // new assignee
	}

	// Send email
	templateType := "QC Update"
	var templateID int = 7
	if err := emailService.SendTemplatedEmail(templateType, emailData, &templateID); err != nil {
		log.Printf("Failed to send notification email: %v", err)
	}

	return nil
}

func updateCompleteProduction(tx *sql.Tx, activityID, userID int, status string) error {
	_, err := tx.Exec(`
        UPDATE complete_production
        SET updated_at = $1, status = $2
        WHERE activity_id = $3 AND user_id = $4
    `, time.Now(), status, activityID, userID)
	if err != nil {
		return fmt.Errorf("failed to update complete_production: %v", err)
	}
	log.Printf("40")
	return nil
}

// func sendNotifications(db *sql.DB, fcmService *services.FCMService, activity struct {
// 	ID                    int
// 	TaskID                int
// 	QCID                  sql.NullInt64
// 	StageID               int
// 	ProjectID             int
// 	Status                string
// 	MeshMoldStatus        string
// 	ReinforcementStatus   string
// 	MeshMoldQCStatus      string
// 	ReinforcementQCStatus string
// 	ElementID             int
// 	StockyardID           int
// }, status, currStage string) {
// 	if fcmService == nil {
// 		return
// 	}

// 	// Get current stage name
// 	// var currentStageName string
// 	// if err := db.QueryRow(`SELECT name FROM project_stages WHERE id = $1`, activity.StageID).Scan(&currentStageName); err != nil {
// 	// 	log.Printf("Error fetching current stage name: %v", err)
// 	// 	currentStageName = "current stage" // Default value
// 	// }

// 	// Get element type name
// 	var elementTypeName string
// 	if err := db.QueryRow(`
//         SELECT et.element_type
//         FROM element_type et
//         JOIN task t ON t.element_type_id = et.element_type_id
//         WHERE t.task_id = $1`, activity.TaskID).Scan(&elementTypeName); err != nil {
// 		log.Printf("Error fetching element type name: %v", err)
// 		elementTypeName = "element" // Default value
// 	}

// 	// Send notification to next stage assignee if task is completed
// 	if strings.EqualFold(status, "completed") {
// 		var nextStageName string
// 		var nextStageAssignedTo int

// 		if err := db.QueryRow(`
//             SELECT ps.name, ps.assigned_to
//             FROM project_stages ps
//             WHERE ps.id = (
//                 SELECT stage_id FROM activity WHERE id = $1
//             )`, activity.ID).Scan(&nextStageName, &nextStageAssignedTo); err != nil {
// 			log.Printf("Error fetching next stage details: %v", err)
// 		} else if nextStageAssignedTo > 0 {
// 			sendNotificationToUser(db, fcmService, nextStageAssignedTo,
// 				"Task Assigned",
// 				fmt.Sprintf("%s QC has approved the task. It is now your task for %s stage (Element: %s)",
// 					currStage, nextStageName, elementTypeName),
// 				activity.ID, activity.TaskID, activity.ProjectID, nextStageName)
// 		}
// 	}

// 	// Send notification to QC if task is completed
// 	var qcID int
// 	if err := db.QueryRow(`SELECT qc_id FROM project_stages WHERE id = $1`, activity.StageID).Scan(&qcID); err != nil {
// 		log.Printf("Error fetching QC ID: %v", err)
// 	} else if qcID > 0 {
// 		sendNotificationToUser(db, fcmService, qcID,
// 			"Task Ready for QC",
// 			fmt.Sprintf("Task is ready for QC in %s stage (Element: %s)",
// 				currStage, elementTypeName),
// 			activity.ID, activity.TaskID, activity.ProjectID, currStage)
// 	}
// }

// func sendNotificationToUser(db *sql.DB, fcmService *services.FCMService, userID int,
// 	title, message string,
// 	activityID, taskID, projectID int, stageName string) {

// 	var fcmToken string
// 	if err := db.QueryRow(`SELECT fcm_token FROM users WHERE id = $1 AND fcm_token IS NOT NULL`, userID).Scan(&fcmToken); err != nil {
// 		log.Printf("Error fetching FCM token for user %d: %v", userID, err)
// 		return
// 	}

// 	if fcmToken == "" {
// 		return
// 	}

// 	err := fcmService.SendMulticastNotification(
// 		context.Background(), // Using background context since we don't have request context here
// 		[]string{fcmToken},
// 		title,
// 		message,
// 		map[string]string{
// 			"activity_id": strconv.Itoa(activityID),
// 			"task_id":     strconv.Itoa(taskID),
// 			"project_id":  strconv.Itoa(projectID),
// 			"stage_name":  stageName,
// 		},
// 	)
// 	if err != nil {
// 		log.Printf("Error sending push notification to user %d: %v", userID, err)
// 	}
// }

// GetAnswers godoc
// @Summary      Get answers by stage, task, project
// @Tags         questions
// @Param        stage_id   path  int  true  "Stage ID"
// @Param        task_id    path  int  true  "Task ID"
// @Param        project_id path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/questions/answers/{stage_id}/{task_id}/{project_id} [get]
func GetAnswers(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Retrieve stage_id, task_id, and project_id from the URL parameters
		stageID := c.Param("stage_id")
		taskID := c.Param("task_id")
		projectID := c.Param("project_id")

		// Query to fetch all answers along with related details from project_column, activity, option, question, and users tables
		query := `
			SELECT 
				qc_answers.id, 
				qc_answers.project_id, 
				qc_answers.qc_id, 
				qc_answers.question_id, 
				qc_answers.option_id, 
				qc_answers.task_id, 
				qc_answers.stage_id, 
				qc_answers.comment, 
				qc_answers.image_path, 
				qc_answers.created_at, 
				qc_answers.updated_at,
				pc.name as stage_name,
				a.name as activity_name,
				o.option_text,
				q.question_text,
				u.id as qc_user_id, 
				u.first_name, 
				u.last_name, 
				u.email
			FROM 
				qc_answers
			JOIN 
				project_column pc ON qc_answers.stage_id = pc.id
			JOIN 
				activity a ON qc_answers.task_id = a.id
			JOIN 
				options o ON qc_answers.option_id = o.id
			JOIN 
				questions q ON qc_answers.question_id = q.id
			JOIN 
				users u ON qc_answers.qc_id = u.id
			WHERE 
				qc_answers.stage_id = $1 
				AND qc_answers.task_id = $2 
				AND qc_answers.project_id = $3`

		// Query the database
		rows, err := db.Query(query, stageID, taskID, projectID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer rows.Close()

		// Create a slice to hold the answers with related details
		var answers []struct {
			ID           int       `json:"id"`
			ProjectID    int       `json:"project_id"`
			QCID         int       `json:"qc_id"`
			QuestionID   int       `json:"question_id"`
			OptionID     *int      `json:"option_id"`
			TaskID       int       `json:"task_id"`
			StageID      int       `json:"stage_id"`
			Comment      *string   `json:"comment"`
			ImagePath    *string   `json:"image_path"`
			CreatedAt    time.Time `json:"created_at"`
			UpdatedAt    time.Time `json:"updated_at"`
			StageName    string    `json:"stage_name"`
			ActivityName string    `json:"activity_name"`
			OptionText   string    `json:"option_text"`
			QuestionText string    `json:"question_text"`
			QCUser       struct {
				ID        int    `json:"id"`
				FirstName string `json:"first_name"`
				LastName  string `json:"last_name"`
				Email     string `json:"email"`
			} `json:"qc_user"`
		}

		// Iterate over the rows and populate the slice
		for rows.Next() {
			var answer struct {
				ID           int       `json:"id"`
				ProjectID    int       `json:"project_id"`
				QCID         int       `json:"qc_id"`
				QuestionID   int       `json:"question_id"`
				OptionID     *int      `json:"option_id"`
				TaskID       int       `json:"task_id"`
				StageID      int       `json:"stage_id"`
				Comment      *string   `json:"comment"`
				ImagePath    *string   `json:"image_path"`
				CreatedAt    time.Time `json:"created_at"`
				UpdatedAt    time.Time `json:"updated_at"`
				StageName    string    `json:"stage_name"`
				ActivityName string    `json:"activity_name"`
				OptionText   string    `json:"option_text"`
				QuestionText string    `json:"question_text"`
				QCUser       struct {
					ID        int    `json:"id"`
					FirstName string `json:"first_name"`
					LastName  string `json:"last_name"`
					Email     string `json:"email"`
				} `json:"qc_user"`
			}
			if err := rows.Scan(
				&answer.ID, &answer.ProjectID, &answer.QCID, &answer.QuestionID, &answer.OptionID,
				&answer.TaskID, &answer.StageID, &answer.Comment, &answer.ImagePath,
				&answer.CreatedAt, &answer.UpdatedAt, &answer.StageName,
				&answer.ActivityName, &answer.OptionText, &answer.QuestionText,
				&answer.QCUser.ID, &answer.QCUser.FirstName,
				&answer.QCUser.LastName, &answer.QCUser.Email,
			); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			answers = append(answers, answer)
		}

		projectid, err := strconv.Atoi("projectID")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"err": err.Error()})
			return
		}

		// Return the list of answers along with related details
		c.JSON(http.StatusOK, answers)

		log := models.ActivityLog{
			EventContext: "Answers",
			EventName:    "Get",
			Description:  "Get Answers",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectid,
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// GetAllPapers godoc
// @Summary      Get all papers by project ID
// @Tags         questions
// @Param        project_id  path  int  true  "Project ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/questions/papers/{project_id} [get]
func GetAllPapers(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Get project_id from route parameter
		projectIDStr := c.Param("project_id")
		if projectIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "ProjectID is required"})
			return
		}

		// Convert project_id to integer
		var projectIDInt int
		_, err = fmt.Sscanf(projectIDStr, "%d", &projectIDInt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ProjectID"})
			return
		}

		// Query to get all papers by the given project_id
		rows, err := db.Query("SELECT id, name, project_id FROM papers WHERE project_id = $1", projectIDInt)
		if err != nil {
			log.Println("Failed to query database:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query database"})
			return
		}
		defer rows.Close()

		// Collect papers into a slice
		var papers []models.Paper
		for rows.Next() {
			var paper models.Paper
			if err := rows.Scan(&paper.ID, &paper.Name, &paper.ProjectID); err != nil {
				log.Println("Failed to scan row:", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read database result"})
				return
			}
			papers = append(papers, paper)
		}

		// Check if no papers found
		if len(papers) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "No papers found for the given ProjectID"})
			return
		}

		// Respond with the list of papers
		c.JSON(http.StatusOK, papers)

		log := models.ActivityLog{
			EventContext: "Answers",
			EventName:    "Get",
			Description:  "Get All Answers",
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectIDInt,
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// UpdateSingleQuestionHandler godoc
// @Summary      Update single question
// @Tags         questions
// @Accept       json
// @Produce      json
// @Param        question_id  path  int  true  "Question ID"
// @Param        body          body  object  true  "Question data"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/questions/update_questions/{question_id} [put]
func UpdateSingleQuestionHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Extract question_id from URL
		questionIDParam := c.Param("question_id")
		questionID, err := strconv.Atoi(questionIDParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid question_id"})
			return
		}

		// Input structure for update with option IDs included
		var req struct {
			QuestionText *string `json:"question_text,omitempty"` // Optional
			Options      *[]struct {
				OptionID   *int   `json:"option_id,omitempty"` // Optional; nil means new option
				OptionText string `json:"option_text"`         // Required
			} `json:"options,omitempty"` // Optional
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Start transaction
		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}

		// Update question_text if provided
		if req.QuestionText != nil {
			_, err := tx.Exec(`UPDATE questions SET question_text = $1 WHERE id = $2`, *req.QuestionText, questionID)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update question text"})
				return
			}
		}

		if req.Options != nil {
			// Fetch existing option IDs
			rows, err := tx.Query(`SELECT id FROM options WHERE question_id = $1`, questionID)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch existing options"})
				return
			}

			existingOptionIDs := map[int]bool{}
			for rows.Next() {
				var oid int
				if err := rows.Scan(&oid); err != nil {
					rows.Close()
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan option id"})
					return
				}
				existingOptionIDs[oid] = true
			}
			rows.Close()

			requestedOptionIDs := map[int]bool{}

			// Insert or update options
			for _, opt := range *req.Options {
				if opt.OptionID != nil {
					// Update existing option text
					_, err = tx.Exec(`UPDATE options SET option_text = $1 WHERE id = $2 AND question_id = $3`,
						opt.OptionText, *opt.OptionID, questionID)
					if err != nil {
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update option", "option_id": *opt.OptionID})
						return
					}
					requestedOptionIDs[*opt.OptionID] = true
				} else {
					// Insert new option
					_, err = tx.Exec(`INSERT INTO options (question_id, option_text) VALUES ($1, $2)`, questionID, opt.OptionText)
					if err != nil {
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert new option"})
						return
					}
				}
			}

			// Delete options that are not in the request
			for oid := range existingOptionIDs {
				if !requestedOptionIDs[oid] {
					_, err = tx.Exec(`DELETE FROM options WHERE id = $1 AND question_id = $2`, oid, questionID)
					if err != nil {
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete option", "option_id": oid})
						return
					}
				}
			}
		}

		// Commit
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Question updated successfully", "question_id": questionID})

		// Get project_id and project name for notification
		var projectID int
		var projectName string
		err = db.QueryRow(`
			SELECT q.project_id, p.name 
			FROM questions q
			JOIN project p ON q.project_id = p.project_id
			WHERE q.id = $1
		`, questionID).Scan(&projectID, &projectName)
		if err != nil {
			log.Printf("Failed to fetch project details: %v", err)
			projectName = "Unknown Project"
		}

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who updated the question
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("Question updated in project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", projectID),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.Exec(`
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all project members, clients, and end_clients
		if projectID > 0 {
			sendProjectNotifications(db, projectID,
				fmt.Sprintf("Question updated in project: %s", projectName),
				fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", projectID))
		}

		log := models.ActivityLog{
			EventContext: "Question",
			EventName:    "Update",
			Description:  "Update question" + questionIDParam,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// UpdateQuestions godoc
// @Summary      Update paper (questions)
// @Tags         questions
// @Accept       json
// @Produce      json
// @Param        paper_id  path  int  true  "Paper ID"
// @Param        body      body  object  true  "Paper/questions data"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/questions/update_paper/{paper_id} [put]
func UpdateQuestions(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		paperIDParam := c.Param("paper_id")
		paperID, err := strconv.Atoi(paperIDParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid paper_id"})
			return
		}

		// Request body structure including OptionID for updating
		var req struct {
			PaperName string `json:"paper_name"`
			ProjectID int    `json:"project_id"`
			Questions []struct {
				QuestionID   int    `json:"question_id"`
				QuestionText string `json:"question_text"`
				Options      []struct {
					OptionID   *int   `json:"option_id,omitempty"` // optional, nil for new options
					OptionText string `json:"option_text"`
				} `json:"options"`
			} `json:"questions"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}

		// Optional: Update paper name if needed
		if req.PaperName != "" {
			_, err = tx.Exec(`UPDATE papers SET name = $1 WHERE id = $2`, req.PaperName, paperID)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update paper name"})
				return
			}
		}

		for _, question := range req.Questions {
			// Update question text
			_, err = tx.Exec(
				`UPDATE questions SET question_text = $1 WHERE id = $2 AND paper_id = $3 AND project_id = $4`,
				question.QuestionText, question.QuestionID, paperID, req.ProjectID,
			)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update question", "question_id": question.QuestionID})
				return
			}

			// Fetch existing option IDs for this question
			rows, err := tx.Query(`SELECT id FROM options WHERE question_id = $1`, question.QuestionID)
			if err != nil {
				tx.Rollback()
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch existing options", "question_id": question.QuestionID})
				return
			}

			existingOptionIDs := map[int]bool{}
			for rows.Next() {
				var oid int
				if err := rows.Scan(&oid); err != nil {
					rows.Close()
					tx.Rollback()
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan option id", "question_id": question.QuestionID})
					return
				}
				existingOptionIDs[oid] = true
			}
			rows.Close()

			requestedOptionIDs := map[int]bool{}

			// Insert or update options
			for _, opt := range question.Options {
				if opt.OptionID != nil {
					// Update existing option text
					_, err = tx.Exec(`UPDATE options SET option_text = $1 WHERE id = $2 AND question_id = $3`,
						opt.OptionText, *opt.OptionID, question.QuestionID)
					if err != nil {
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update option", "option_id": *opt.OptionID})
						return
					}
					requestedOptionIDs[*opt.OptionID] = true
				} else {
					// Insert new option
					_, err = tx.Exec(`INSERT INTO options (question_id, option_text) VALUES ($1, $2)`, question.QuestionID, opt.OptionText)
					if err != nil {
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert new option", "question_id": question.QuestionID})
						return
					}
				}
			}

			// Delete options that are not in the request
			for oid := range existingOptionIDs {
				if !requestedOptionIDs[oid] {
					_, err = tx.Exec(`DELETE FROM options WHERE id = $1 AND question_id = $2`, oid, question.QuestionID)
					if err != nil {
						tx.Rollback()
						c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete option", "option_id": oid})
						return
					}
				}
			}
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "Questions updated successfully",
			"paper_id":   paperID,
			"project_id": req.ProjectID,
		})

		// Get project name for notification
		var projectName string
		err = db.QueryRow("SELECT name FROM project WHERE project_id = $1", req.ProjectID).Scan(&projectName)
		if err != nil {
			log.Printf("Failed to fetch project name: %v", err)
			projectName = fmt.Sprintf("Project %d", req.ProjectID)
		}

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who updated the questions
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("Questions updated for paper: %s in project: %s", req.PaperName, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", req.ProjectID),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.Exec(`
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all project members, clients, and end_clients
		sendProjectNotifications(db, req.ProjectID,
			fmt.Sprintf("Questions updated for paper: %s in project: %s", req.PaperName, projectName),
			fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", req.ProjectID))

		log := models.ActivityLog{
			EventContext: "Question",
			EventName:    "Update",
			Description:  "update question of" + req.PaperName,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    req.ProjectID,
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// DeleteSingleQuestionHandler godoc
// @Summary      Delete single question
// @Tags         questions
// @Param        question_id  path  int  true  "Question ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/questions/delete/{question_id} [delete]
func DeleteSingleQuestionHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {

		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		questionIDParam := c.Param("question_id")
		questionID, err := strconv.Atoi(questionIDParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid question ID"})
			return
		}

		// Get project_id and project name before deletion
		var projectID int
		var projectName string
		err = db.QueryRow(`
			SELECT q.project_id, p.name 
			FROM questions q
			JOIN project p ON q.project_id = p.project_id
			WHERE q.id = $1
		`, questionID).Scan(&projectID, &projectName)
		if err != nil {
			log.Printf("Failed to fetch project details: %v", err)
			projectName = "Unknown Project"
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}

		// Delete options first due to foreign key constraint
		_, err = tx.Exec(`DELETE FROM options WHERE question_id = $1`, questionID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete options"})
			return
		}

		// Then delete question
		_, err = tx.Exec(`DELETE FROM questions WHERE id = $1`, questionID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete question"})
			return
		}

		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Question deleted successfully", "question_id": questionID})

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who deleted the question
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("Question deleted in project: %s", projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", projectID),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.Exec(`
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all project members, clients, and end_clients
		if projectID > 0 {
			sendProjectNotifications(db, projectID,
				fmt.Sprintf("Question deleted in project: %s", projectName),
				fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", projectID))
		}

		log := models.ActivityLog{
			EventContext: "Question",
			EventName:    "Delete",
			Description:  "Delete Question of id:" + questionIDParam,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}

// DeletePaperAndQuestionsHandler godoc
// @Summary      Delete paper and its questions
// @Tags         questions
// @Param        paper_id  path  int  true  "Paper ID"
// @Success      200  {object}  object
// @Failure      400  {object}  object
// @Failure      401  {object}  object
// @Router       /api/questions/paper_dalete/{paper_id} [delete]
func DeletePaperAndQuestionsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sessionID := c.GetHeader("Authorization")
		if sessionID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "session-id header is required"})
			return
		}

		session, userName, err := GetSessionDetails(db, sessionID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid session", "details": err.Error()})
			return
		}

		// Get paper_id from URL param
		paperIDParam := c.Param("paper_id")
		paperID, err := strconv.Atoi(paperIDParam)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid paper_id"})
			return
		}

		// Get project_id and project name before deletion
		var projectID int
		var projectName, paperName string
		err = db.QueryRow(`
			SELECT p.project_id, pr.name, p.name
			FROM papers p
			JOIN project pr ON p.project_id = pr.project_id
			WHERE p.id = $1
		`, paperID).Scan(&projectID, &projectName, &paperName)
		if err != nil {
			log.Printf("Failed to fetch project details: %v", err)
			projectName = "Unknown Project"
			paperName = fmt.Sprintf("Paper %d", paperID)
		}

		tx, err := db.Begin()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to start transaction"})
			return
		}

		// Delete options linked to questions in this paper
		_, err = tx.Exec(`DELETE FROM options WHERE question_id IN (SELECT id FROM questions WHERE paper_id = $1)`, paperID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete options"})
			return
		}

		// Delete questions linked to this paper
		_, err = tx.Exec(`DELETE FROM questions WHERE paper_id = $1`, paperID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete questions"})
			return
		}

		// Delete the paper itself
		result, err := tx.Exec(`DELETE FROM papers WHERE id = $1`, paperID)
		if err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete paper"})
			return
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			tx.Rollback()
			c.JSON(http.StatusNotFound, gin.H{"error": "Paper not found"})
			return
		}

		// Commit the transaction
		if err := tx.Commit(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Paper and all related questions deleted successfully", "paper_id": paperID})

		// Get userID from session for notification
		var adminUserID int
		err = db.QueryRow("SELECT user_id FROM session WHERE session_id = $1", sessionID).Scan(&adminUserID)
		if err != nil {
			log.Printf("Failed to fetch user_id for notification: %v", err)
		} else {
			// Send notification to the admin user who deleted the paper
			notif := models.Notification{
				UserID:    adminUserID,
				Message:   fmt.Sprintf("Paper deleted: %s in project: %s", paperName, projectName),
				Status:    "unread",
				Action:    fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", projectID),
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			_, err = db.Exec(`
				INSERT INTO notifications (user_id, message, status, action, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, notif.UserID, notif.Message, notif.Status, notif.Action, notif.CreatedAt, notif.UpdatedAt)

			if err != nil {
				log.Printf("Failed to insert notification: %v", err)
			}
		}

		// Send notifications to all project members, clients, and end_clients
		if projectID > 0 {
			sendProjectNotifications(db, projectID,
				fmt.Sprintf("Paper deleted: %s in project: %s", paperName, projectName),
				fmt.Sprintf("https://precastezy.blueinvent.com/project/%d/plan", projectID))
		}

		log := models.ActivityLog{
			EventContext: "Paper",
			EventName:    "Delete",
			Description:  "Delete paper" + paperIDParam,
			UserName:     userName,
			HostName:     session.HostName,
			IPAddress:    session.IPAddress,
			CreatedAt:    time.Now(),
			ProjectID:    projectID,
		}

		// Step 5: Insert activity log
		if logErr := SaveActivityLog(db, log); logErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "Failed to log activity",
				"details": logErr.Error(),
			})
			return
		}
	}
}
