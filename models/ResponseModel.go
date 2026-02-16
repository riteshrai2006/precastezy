package models

import (
	"time"

	_ "github.com/lib/pq"
)

type DrawingsRevisionResponse struct {
	Version           string `json:"version"`
	DrawingTypeId     int    `json:"drawing_type_id"`
	DrawingTypeName   string `json:"drawing_type_name"`
	Comments          string `json:"comments"`
	File              string `json:"file"`
	DrawingRevisionId int    `json:"drawing_revision_id"`
}

type DrawingResponse struct {
	DrawingId        int                        `json:"drawing_id"`
	ProjectId        int                        `json:"-"`
	CurrentVersion   string                     `json:"current_version"`
	DrawingTypeId    int                        `json:"drawing_type_id"`
	DrawingTypeName  string                     `json:"drawing_type_name"`
	Comments         string                     `json:"comments"`
	File             string                     `json:"file"`
	CreatedAt        time.Time                  `json:"-"`
	CreatedBy        string                     `json:"-"`
	UpdateAt         time.Time                  `json:"-"`
	ElementTypeID    int                        `json:"-"`
	ElementTypeName  string                     `json:"-"`
	DrawingsRevision []DrawingsRevisionResponse `json:"drawingsRevision"` // Updated revision response
}
type ElementResponse struct {
	ID                 int               `json:"id"`
	ElementID          string            `json:"element_id"`
	ElementName        string            `json:"element_name"`
	ProjectID          int               `json:"project_id"`
	ElementType        string            `json:"element_type"`
	ElementTypeVersion string            `json:"element_type_version"`
	ElementTypeID      int               `json:"element_type_id"`
	ElementTypeName    string            `json:"element_type_name"`
	Thickness          float64           `json:"thickness"`
	Length             float64           `json:"length"`
	Height             float64           `json:"height"`
	Volume             float64           `json:"volume"`
	Mass               float64           `json:"mass"`
	Area               float64           `json:"area"`
	Width              float64           `json:"width"`
	CreatedBy          string            `json:"-"`
	CreatedAt          time.Time         `json:"-"`
	UpdatedAt          time.Time         `json:"-"`
	Status             string            `json:"status"` // <-- Add this
	Products           []ProductResponse `json:"-"`
	Drawings           []DrawingResponse `json:"drawings"`
	Tower              string            `json:"tower"`
	Floor              string            `json:"floor"`
}
type ProductResponse struct {
	ProductID   int     `json:"product_id"` // ID of the product
	ProductName string  `json:"product_name"`
	Quantity    float64 `json:"quantity"` // Quantity of the product
}

type BOMResponse struct {
	ID            int               `json:"id"`
	ElementTypeID int               `json:"element_type_id"`
	ProjectId     int               `json:"project_id"`
	CreatedAt     time.Time         `json:"created_at"`
	CreatedBy     string            `json:"created_by"`
	UpdatedAt     time.Time         `json:"updated_at"`
	UpdatedBy     string            `json:"updated_by"`
	Product       []ProductResponse `json:"product"` // Change to a slice of Product
}

type PrecastResponce struct {
	ID               int                `json:"id"`
	ProjectID        int                `json:"project_id"`
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	ParentID         *int               `json:"parent_id"` // Use *int to handle NULL values
	Prefix           string             `json:"prefix"`
	NamingConvention string             `json:"naming_convention"`
	Others           bool               `json:"others"`
	Children         []*PrecastResponce `json:"children,omitempty"` // To hold child responses if needed
}
type ElementTypeName struct {
	ElementTypeID   int    `json:"element_type_id"`
	ElementType     string `json:"element_type"`
	ElementTypeName string `json:"element_type_name"`
}
type ElementTypeR struct {
	ElementType        string               `json:"element_type"`
	ElementTypeName    string               `json:"element_type_name" `
	Thickness          float64              `json:"thickness" `
	Length             float64              `json:"length" `
	Height             float64              `json:"height"`
	Volume             float64              `json:"volume" `
	Mass               float64              `json:"mass" `
	Area               float64              `json:"area" `
	Width              float64              `json:"width" `
	CreatedBy          string               `json:"created_by"`
	CreatedAt          time.Time            `json:"-"`
	UpdatedAt          time.Time            `json:"-"`
	ElementTypeId      int                  `json:"element_type_id"`
	ProjectID          int                  `json:"project_id" `
	ElementTypeVersion string               `json:"element_type_version"`
	TotalCountElement  int                  `json:"total_count_element"`
	CreatedAtFormatted string               `json:"created_at_formatted"`
	UpdatedAtFormatted string               `json:"updated_at_formatted"`
	TowerName          string               `json:"tower_name"`
	FloorName          string               `json:"floor_name"`
	HierarchyQ         []HierarchyQuantityR `json:"-"`
	HierarchyResponce  []HierarchyResponce  `json:"hierarchy_quantity"`
	Drawings           []DrawingsR          `json:"drawings"`
	Products           []ProductR           `json:"products"`
}
// ElementTypePaginationResponse is used in @Success for paginated element types list (swagger)
type ElementTypePaginationResponse struct {
	Data       []ElementTypeResponse `json:"data"`
	Pagination Pagination            `json:"pagination"`
}

// ElementTypeResponse is used in @Success for element type create/get (swagger)
type ElementTypeResponse struct {
	ElementType        string               `json:"element_type"`
	ElementTypeName    string               `json:"element_type_name"`
	Thickness          float64              `json:"thickness"`
	Length             float64              `json:"length"`
	Height             float64              `json:"height"`
	Volume             float64              `json:"volume"`
	Mass               float64              `json:"mass"`
	Area               float64              `json:"area"`
	Width              float64              `json:"width"`
	ElementTypeID      int                  `json:"element_type_id"`
	ProjectID          int                  `json:"project_id"`
	ElementTypeVersion string               `json:"element_type_version"`
	TotalCountElement  int                  `json:"total_count_element"`
	HierarchyQuantity  []HierarchyResponce  `json:"hierarchy_quantity,omitempty"`
	Drawings           []DrawingsR          `json:"drawings,omitempty"`
	Products           []ProductR           `json:"products,omitempty"`
}

type ElementTypeWithHierarchyResponse struct {
	ElementType        string      `json:"element_type"`
	ElementTypeName    string      `json:"element_type_name"`
	Thickness          float64     `json:"thickness"`
	Length             float64     `json:"length"`
	Height             float64     `json:"height"`
	Volume             float64     `json:"volume"`
	Mass               float64     `json:"mass"`
	Area               float64     `json:"area"`
	Width              float64     `json:"width"`
	ElementTypeID      int         `json:"element_type_id"`
	ProjectID          int         `json:"project_id"`
	ElementTypeVersion string      `json:"element_type_version"`
	Quantity           int         `json:"quantity"`
	HierarchyID        int         `json:"hierarchy_id"` // Changed from HierarchyItem to match query
	FloorName          string      `json:"floor_name"`
	TowerName          string      `json:"tower_name"`
	NamingConvention   string      `json:"naming_convention"`
	Drawings           []DrawingsR `json:"drawings"`
	ProductionCount    int         `json:"production_count"`
	StockyardCount     int         `json:"stockyard_count"`
	InRequestCount     int         `json:"in_request_count"`
	DispatchCount      int         `json:"dispatch_count"`
	ErectionCount      int         `json:"erection_count"`
}

type HierarchyQuantityR struct {
	HierarchyId      int    `json:"hierarchy_id"`
	Quantity         int    `json:"quantity"`
	NamingConvention string `json:"naming_convention"`
}
type HierarchyResponce struct {
	HierarchyID      int     `json:"hierarchy_id"`
	Quantity         int     `json:"quantity"`
	ProjectID        int     `json:"project_id"`
	Name             string  `json:"name"`
	Description      string  `json:"-"`
	ParentID         int     `json:"parent_id"`
	Prefix           string  `json:"-"`
	NamingConvention string  `json:"naming_convention"`
	TowerName        *string `json:"tower_name"`
	FloorName        string  `json:"floor_name"`
}

type DrawingsR struct {
	DrawingsId       int                 `json:"drawing_id"`
	UpdatedBy        string              `json:"updated_by"`
	CurrentVersion   string              `json:"current_version"`
	CreatedAt        time.Time           `json:"created_at"`
	CreatedBy        string              `json:"created_by"`
	DrawingTypeId    int                 `json:"drawing_type_id"`
	DrawingTypeName  string              `json:"drawing_type_name"`
	UpdatedAt        time.Time           `json:"updated_at"`
	Comments         string              `json:"comments"`
	File             string              `json:"file"`
	ElementTypeID    int                 `json:"Element_type_id"`
	DrawingsRevision []DrawingsRevisionR `json:"drawingsRevision"`
}
type DrawingsRevisionR struct {
	ParentDrawingsId   int       `json:"parent_drawing_id"`
	Version            string    `json:"version"`
	CreatedAt          time.Time `json:"-"`
	CreatedBy          string    `json:"created_by"`
	DrawingsTypeId     int       `json:"drawing_type_id"`
	DrawingTypeName    string    `json:"drawing_type_name"`
	Comments           string    `json:"comments"`
	File               string    `json:"file"`
	DrawingsRevisionId int       `json:"drawing_revision_id"`
	ElementTypeID      int       `json:"Element_type_id"`
	CreatedAtFormatted string    `json:"created_at_formatted"`
	UpdatedAtFormatted string    `json:"updated_at_formatted"`
}
type ProductR struct {
	ProductID   int     `json:"product_id"` // ID of the product
	ProductName string  `json:"product_name"`
	Quantity    float64 `json:"quantity"` // Quantity of the product
}
type BOMProR struct {
	ID            int       `json:"id"`
	ElementTypeID int       `json:"element_type_id"`
	ProjectId     int       `json:"project_id"`
	CreatedAt     time.Time `json:"created_at"`
	CreatedBy     string    `json:"created_by"`
	UpdatedAt     time.Time `json:"updated_at"`
	UpdatedBy     string    `json:"updated_by"`
	Product       string    `json:"product"`
}
type InventoryViewResponse struct {
	BomId          int    `json:"bom_id"`
	BomQty         int    `json:"bom_qty"`
	WarehouseNames string `json:"warehouse_names"`
	BomName        string `json:"bom_name"`
}

// WarehouseDetails struct to represent each warehouse and its associated bom_qty
type WarehouseDetails struct {
	WarehouseName string `json:"warehouse_name"`
	BomQty        int    `json:"bom_qty"`
}

// BomInventoryResponse struct to represent the final response
type BomInventoryResponse struct {
	BomId         int                `json:"bom_id"`
	BomQty        int                `json:"-"`
	BomName       string             `json:"bom_name"`
	WarehouseData []WarehouseDetails `json:"warehouse_data"` // Add this field
}
type Item struct {
	FloorID         int    `json:"floor_id"`
	ElementType     string `json:"element_type"`
	ElementTypeId   int    `json:"element_type_id" `
	ElementTypeName string `json:"element_type_name" `
	TotalQuantity   int    `json:"total_quantity"`
	BalanceQuantity int    `json:"Balance_quantity"`
}

// Floor represents dynamic categories like "Wall", "Floor", "Roof"
type Floor map[string][]Item // Dynamic categories (e.g., "Wall", "Floor", "Roof")

// Tower represents floors in a building
type Tower map[string]Floor // Dynamic floors (e.g., "Floor 1", "Floor 2")

// Building represents a collection of towers
type Building map[string]Tower // Dynamic towers (e.g., "Tower 1", "Tower 2")

type PrecastStockResponse struct {
	ID              int       `json:"id"`
	ElementID       int       `json:"element_id"`
	ElementType     string    `json:"element_type"`
	ElementTypeID   int       `json:"element_type_id"`
	Dimensions      string    `json:"dimensions"`
	Volume          float64   `json:"volume"`
	Mass            float64   `json:"mass"`
	Area            float64   `json:"area"`
	Width           float64   `json:"width"`
	ProductionDate  time.Time `json:"production_date"`
	StorageLocation string    `json:"storage_location,omitempty"`
	ProjectID       int       `json:"project_id"`
	TargetLocation  int       `json:"target_location"`
	Disable         bool      `json:"disable"`
}

type ItemResponce struct {
	ID            int    `json:"id"`
	ElementID     int    `json:"element_id"`
	ElementType   string `json:"element_type"`
	ElementTypeID int    `json:"element_type_id"`
	ProjectID     int    `json:"project_id"`
}

// ItemResponse represents an individual item in the stockyard
type ItemResponse struct {
	ID              int    `json:"id"`
	ElementID       int    `json:"element_id"`
	ElementType     string `json:"element_type"`
	ElementTypeID   int    `json:"element_type_id"`
	ElementTypeName string `json:"element_type_name"`
	ProjectID       int    `json:"project_id"`
}

// Floor represents a collection of element types under a floor
type FloorResponse map[string][]ItemResponse // Example: "Wall" → []ItemResponse

// FloorInfoResponse represents individual floor information
type FloorInfoResponse struct {
	ID          int    `json:"hierarchy_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    int    `json:"parent_id"`
	TowerName   string `json:"tower_name"`
	Others      bool   `json:"others"`
}

// Tower represents a collection of floors under a tower
type TowerResponse map[string]FloorResponse // Example: "Floor 1" → Floor

// Building represents a collection of towers
type BuildingResponse map[string]TowerResponse // Example: "Tower G6" → Tower

type ElementCountResponse struct {
	ElementType      string `json:"element_type"`
	ElementTypeID    int    `json:"element_type_id"`
	ElementTypeName  string `json:"element_type_name"`
	BalancelElements int    `json:"Balance_elements"`
	Leftelements     int    `json:"left _elements"`
	FloorID          int    `json:"floor_id"`
	Disable          bool   `json:"disable"`
}
type ErectionOrderResponce struct {
	PrecastStockID    int     `json:"precast_stock_id" example:"101"`
	ElementID         int     `json:"element_id" example:"2001"`
	ElementTypeID     int     `json:"element_type_id" example:"5"`
	ElementTypeName   string  `json:"element_type_name" example:"Steel Beam"`
	ElementType       string  `json:"element_type" example:"Beam"`
	ElementTypeWeight float64 `json:"element_type_weight" example:"150.5"`
	FloorName         string  `json:"floor_name" example:"Ground Floor"`
	TowerName         string  `json:"tower_name" example:"Tower A"`
	FloorID           int     `json:"floor_id" example:"10"`
	ElementName       string  `json:"element_name" example:"Beam 1"`
	Disable           bool    `json:"disable" example:"false"`
	Status            string  `json:"status" example:"Approved"`
}
type StockSummaryResponce struct {
	ElementType      string  `json:"element_type"`
	ElementTypeID    int     `json:"element_type_id"`
	ElementTypeName  string  `json:"element_type_name"`
	StockElementID   string  `json:"stock_element_id"`
	ElementTableID   int     `json:"element_table_id"`
	ElementElementID string  `json:"element_element_id"`
	TowerName        string  `json:"tower_name"`
	FloorName        string  `json:"floor_name"`
	FloorID          int     `json:"floor_id"`
	Weight           float64 `json:"weight"`
	Disable          bool    `json:"disable"`
}

type ElementQRResponse struct {
	ID                 int     `json:"id"`
	ElementID          string  `json:"element_id"`
	ElementName        string  `json:"element_name"`
	ProjectID          int     `json:"project_id"`
	ElementType        string  `json:"element_type"`
	ElementTypeVersion string  `json:"element_type_version"`
	ElementTypeID      int     `json:"element_type_id"`
	ElementTypeName    string  `json:"element_type_name"`
	Thickness          float64 `json:"thickness"`
	Length             float64 `json:"length"`
	Height             float64 `json:"height"`
	Weight             float64 `json:"weight"`
	Floor              string  `json:"floor"`
	Tower              string  `json:"tower"`
	FloorID            int     `json:"floor_id"`
	TowerID            int     `json:"tower_id"`
}
type DispatchOrderPDF struct {
	ID            int
	OrderNumber   string
	ProjectID     int
	DispatchDate  time.Time
	VehicleID     int
	DriverName    string
	CurrentStatus string
	VehicleNumber string
	Model         string
	Manufacturer  string
	Capacity      string
	ProjectName   string
}

type DispatchItem struct {
	ElementID       int
	ElementType     string
	Dimensions      string
	Weight          float64
	ElementTypeName string
}
type ReceiveDispatchRequest struct {
	ReceivedAt    time.Time `json:"received_at" binding:"required"`
	ReceivedBy    string    `json:"received_by" binding:"required"`
	Notes         string    `json:"notes"`
	Condition     string    `json:"condition" binding:"required"` // e.g., "Good", "Damaged", "Partial"
	DamageDetails string    `json:"damage_details,omitempty"`     // Required if condition is "Damaged"
}

// DispatchOrderResponse represents the enhanced response structure for dispatch orders
type DispatchOrderResponse struct {
	ID            int                    `json:"id"`
	OrderNumber   string                 `json:"dispatch_order_id"`
	ProjectID     int                    `json:"project_id"`
	ProjectName   string                 `json:"project_name"`
	DispatchDate  time.Time              `json:"dispatch_date"`
	VehicleId     int                    `json:"vehicle_id"`
	DriverName    string                 `json:"driver_name"`
	CurrentStatus string                 `json:"current_status"`
	Items         []DispatchItemResponse `json:"items"`
}

// DispatchItemResponse represents the enhanced response structure for dispatch items
type DispatchItemResponse struct {
	ElementID       int     `json:"element_id"`
	ElementType     string  `json:"element_type"`
	ElementTypeName string  `json:"element_type_name"`
	Weight          float64 `json:"weight"`
}

// QRCodeResponse represents the simplified data structure for QR codes
type QRCodeResponse struct {
	ID        int    `json:"id"`
	ElementID string `json:"element_id"`
}
type PrecastStockResponseDetails struct {
	ID              int       `json:"id"`
	ElementName     string    `json:"element_name"`
	ElementType     string    `json:"element_type"`
	ElementID       int       `json:"element_id"`
	ElementTypeID   int       `json:"element_type_id"`
	ElementTypeName string    `json:"element_type_name"`
	StockyardID     int       `json:"stockyard_id"`
	Thickness       float64   `json:"thickness"`
	Length          float64   `json:"length"`
	Height          float64   `json:"height"`
	Volume          float64   `json:"volume"`
	Mass            float64   `json:"mass"`
	Area            float64   `json:"area"`
	Width           float64   `json:"width"`
	ProductionDate  time.Time `json:"production_date"`
	StorageLocation string    `json:"storage_location"`
	DispatchStatus  bool      `json:"dispatch_status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Stockyard       bool      `json:"stockyard"`
	ProjectID       int       `json:"project_id"`
	TargetLocation  int       `json:"target_location"`
	TowerName       string    `json:"tower_name"`
	FloorName       string    `json:"floor_name"`
	FloorID         int       `json:"floor_id"`
	Disable         bool      `json:"disable"`
}

// StockErectedResponseDetails represents the response structure for stock erected data
type StockErectedResponseDetails struct {
	ID                  int       `json:"id"`
	PrecastStockID      int       `json:"precast_stock_id"`
	ElementID           int       `json:"element_id"`
	Erected             bool      `json:"erected"`
	ApprovedStatus      bool      `json:"approved_status"`
	ProjectID           int       `json:"project_id"`
	OrderAt             time.Time `json:"order_at"`
	ActionApproveReject time.Time `json:"action_approve_reject"`
	Comments            string    `json:"comments"`
	ElementType         string    `json:"element_type"`
	ElementTypeID       int       `json:"element_type_id"`
	ElementTypeName     string    `json:"element_type_name"`
	TowerName           string    `json:"tower_name"`
	FloorName           string    `json:"floor_name"`
	FloorID             int       `json:"floor_id"`
	Deceble             bool      `json:"deceble"`
}

type PaginatedResponse struct {
	Data       interface{} `json:"data"`
	Pagination Pagination  `json:"pagination"`
}

type Pagination struct {
	CurrentPage  int  `json:"current_page"`
	PageSize     int  `json:"page_size"`
	TotalRecords int  `json:"total_records"`
	TotalPages   int  `json:"total_pages"`
	HasNext      bool `json:"has_next"`
	HasPrev      bool `json:"has_prev"`
}

// Swagger / API docs: common request and response models referenced by handler annotations

// ErrorResponse is used in @Failure for error responses
type ErrorResponse struct {
	Error   string `json:"error" example:"Invalid input"`
	Details string `json:"details,omitempty" example:""`
}

// LoginRequest is used in @Param for login body
type LoginRequest struct {
	Email    string `json:"email" binding:"required" example:"user@example.com"`
	Password string `json:"password" binding:"required" example:"password"`
	IP       string `json:"ip" binding:"required" example:"192.168.1.1"`
}

// LoginResponse is used in @Success for login
type LoginResponse struct {
	Message      string      `json:"message" example:"User successfully logged in"`
	AccessToken  string      `json:"access_token" example:"eyJhbGc..."`
	QC           bool        `json:"qc" example:"false"`
	Role         string      `json:"role" example:"admin"`
	User         LoginUser   `json:"user"`
}

// LoginUser is the user object inside LoginResponse
type LoginUser struct {
	ID    int    `json:"id" example:"1"`
	Email string `json:"email" example:"user@example.com"`
}

// SuccessResponse is used in @Success for generic success
type SuccessResponse struct {
	Message string      `json:"message" example:"Success"`
	Data    interface{} `json:"data,omitempty"`
}

// RunningJobsResponse is used in @Success for list running jobs (swagger)
type RunningJobsResponse struct {
	RunningJobs  []int            `json:"running_jobs" example:"1,2,3"`
	Count        int              `json:"count" example:"2"`
	ShuttingDown bool             `json:"shutting_down" example:"false"`
	JobStates    map[string]string `json:"job_states"` // job_id string -> status
}

// JobStatusResponse is used in @Success for get job status (swagger)
type JobStatusResponse struct {
	ID              uint       `json:"id"`
	ProjectID       int        `json:"project_id"`
	JobType         string     `json:"job_type"`
	Status          string     `json:"status"`
	Progress        int        `json:"progress"`
	TotalItems      int        `json:"total_items"`
	ProcessedItems  int        `json:"processed_items"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
	CompletedAt     *string    `json:"completed_at,omitempty"`
	Error           *string    `json:"error,omitempty"`
	Result          *string    `json:"result,omitempty"`
}

// JobResponse is used in @Success for job list (swagger)
type JobResponse struct {
	ID              uint       `json:"id"`
	ProjectID       int        `json:"project_id"`
	JobType         string     `json:"job_type"`
	Status          string     `json:"status"`
	Progress        int        `json:"progress"`
	TotalItems      int        `json:"total_items"`
	ProcessedItems  int        `json:"processed_items"`
	CreatedBy       string     `json:"created_by"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
	CompletedAt     *string    `json:"completed_at,omitempty"`
	Error           *string    `json:"error,omitempty"`
}

// RollbackResponse is used in @Success for terminate/rollback job (swagger)
type RollbackResponse struct {
	Message string `json:"message" example:"Job terminated and rolled back"`
}

// SessionResponse is used in @Success for session endpoint (swagger)
type SessionResponse struct {
	SessionID string `json:"session_id" example:"uuid"`
	UserID    int    `json:"user_id"`
	Email     string `json:"email"`
}

// ValidateSessionResponse is used in @Success for validate session (swagger)
type ValidateSessionResponse struct {
	Valid bool   `json:"valid" example:"true"`
	Email string `json:"email,omitempty"`
}

// UserResponse is used in @Success for user endpoints (swagger)
type UserResponse struct {
	ID         int    `json:"id"`
	Email      string `json:"email"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	RoleID     int    `json:"role_id"`
	RoleName   string `json:"role_name,omitempty"`
	EmployeeID string `json:"employee_id,omitempty"`
}

// CreateUserRequest is used in @Param for create user body (swagger)
type CreateUserRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	RoleID     int    `json:"role_id"`
	EmployeeID string `json:"employee_id,omitempty"`
}

// UpdateUserRequest is used in @Param for update user body (swagger)
type UpdateUserRequest struct {
	Email      string `json:"email,omitempty"`
	FirstName  string `json:"first_name,omitempty"`
	LastName   string `json:"last_name,omitempty"`
	RoleID     *int   `json:"role_id,omitempty"`
	EmployeeID string `json:"employee_id,omitempty"`
}

// PrecastResponse is used in @Success for precast create/update (swagger)
type PrecastResponse struct {
	ID        int    `json:"id"`
	Message   string `json:"message,omitempty"`
}

// UpdateBOMProductResponse is the response for update_bom_products API (swagger)
type UpdateBOMProductResponse struct {
	Message string `json:"message" example:"Product updated successfully"`
}

// MessageResponse is generic response for APIs that return only {"message": "..."}
type MessageResponse struct {
	Message string `json:"message" example:"Success"`
}

// CreateBOMProductResponse is the response for create_bom_products API (swagger)
type CreateBOMProductResponse struct {
	Message  string       `json:"message" example:"BOM products created successfully"`
	Products []BOMProduct `json:"products"`
	Count    int          `json:"count" example:"1"`
}

// CreateClientRequest is the request body for create client API (swagger)
type CreateClientRequest struct {
	Organization   string `json:"organization" example:"Acme Corp"`
	Email          string `json:"email" example:"client@example.com"`
	Password       string `json:"password" example:"password"`
	FirstName      string `json:"first_name" example:"John"`
	LastName       string `json:"last_name" example:"Doe"`
	Address        string `json:"address,omitempty"`
	City           string `json:"city,omitempty"`
	State          string `json:"state,omitempty"`
	Country        string `json:"country,omitempty"`
	ZipCode        string `json:"zip_code,omitempty"`
	PhoneNo        string `json:"phone_no,omitempty"`
	PhoneCode      int    `json:"phone_code,omitempty"`
	ProfilePicture string `json:"profile_picture,omitempty"`
	StoreID        *int   `json:"store_id,omitempty"`
}

// CreateClientResponse is the response for create client API (swagger)
type CreateClientResponse struct {
	Message  string `json:"message" example:"Client created successfully"`
	ClientID int    `json:"client_id" example:"1"`
}

// CreateProjectResponse is the response for create/update project API (swagger)
type CreateProjectResponse struct {
	Message   string `json:"message" example:"Project created successfully"`
	ProjectID int    `json:"project_id" example:"731623920"`
}
