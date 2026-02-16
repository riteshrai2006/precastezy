package models

import (
	"time"

	"gorm.io/gorm"
)

// GORM-compatible models with proper tags

// ImportJobGorm represents the import_jobs table with GORM tags
type ImportJobGorm struct {
	ID              uint       `gorm:"primaryKey;column:id" json:"id"`
	ProjectID       int        `gorm:"column:project_id;not null" json:"project_id"`
	JobType         string     `gorm:"column:job_type;not null" json:"job_type"`
	Status          string     `gorm:"column:status;not null;default:'pending'" json:"status"`
	Progress        int        `gorm:"column:progress;default:0" json:"progress"`
	TotalItems      int        `gorm:"column:total_items;default:0" json:"total_items"`
	ProcessedItems  int        `gorm:"column:processed_items;default:0" json:"processed_items"`
	CreatedBy       string     `gorm:"column:created_by;not null" json:"created_by"`
	CreatedAt       time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
	CompletedAt     *time.Time `gorm:"column:completed_at" json:"completed_at,omitempty"`
	Error           *string    `gorm:"column:error" json:"error,omitempty"`
	Result          *string    `gorm:"column:result" json:"result,omitempty"`
	FilePath        *string    `gorm:"column:file_path" json:"file_path,omitempty"`
	RollbackEnabled bool       `gorm:"column:rollback_enabled;default:false" json:"rollback_enabled"`
}

// TableName specifies the table name for ImportJobGorm
func (ImportJobGorm) TableName() string {
	return "import_jobs"
}

// ElementTypeGorm represents the element_type table with GORM tags
type ElementTypeGorm struct {
	ElementTypeID      int       `gorm:"primaryKey;column:element_type_id" json:"element_type_id"`
	ElementType        string    `gorm:"column:element_type;not null" json:"element_type"`
	ElementTypeName    string    `gorm:"column:element_type_name;not null" json:"element_type_name"`
	Thickness          string    `gorm:"column:thickness;not null" json:"thickness"`
	Length             string    `gorm:"column:length;not null" json:"length"`
	Height             string    `gorm:"column:height;not null" json:"height"`
	Volume             string    `gorm:"column:volume;not null" json:"volume"`
	Mass               string    `gorm:"column:mass;not null" json:"mass"`
	Area               string    `gorm:"column:area;not null" json:"area"`
	Width              string    `gorm:"column:width;not null" json:"width"`
	CreatedBy          string    `gorm:"column:created_by;not null" json:"created_by"`
	CreatedAt          time.Time `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:update_at;not null" json:"update_at"`
	ProjectID          int       `gorm:"column:project_id;not null" json:"project_id"`
	ElementTypeVersion string    `gorm:"column:element_type_version" json:"element_type_version"`
	TotalCountElement  int       `gorm:"column:total_count_element" json:"total_count_element"`
	Density            float64   `gorm:"column:density;type:numeric(10,2);not null" json:"density"`
	JobID              int       `gorm:"column:job_id" json:"job_id"`
}

// TableName specifies the table name for ElementTypeGorm
func (ElementTypeGorm) TableName() string {
	return "element_type"
}

// ElementTypeHierarchyQuantityGorm represents the element_type_hierarchy_quantity table with GORM tags
type ElementTypeHierarchyQuantityGorm struct {
	ID               int    `gorm:"primaryKey;column:id" json:"id"`
	ElementTypeID    int    `gorm:"column:element_type_id;not null" json:"element_type_id"`
	HierarchyId      int    `gorm:"column:hierarchy_id;not null" json:"hierarchy_id"`
	Quantity         int    `gorm:"column:quantity;not null" json:"quantity"`
	NamingConvention string `gorm:"column:naming_convention;not null" json:"naming_convention"`
	ElementTypeName  string `gorm:"column:element_type_name;type:varchar(150)" json:"element_type_name"`
	ElementType      string `gorm:"column:element_type;type:varchar(150)" json:"element_type"`
	LeftQuantity     *int   `gorm:"column:left_quantity;default:0" json:"left_quantity"`
	ProjectID        *int   `gorm:"column:project_id" json:"project_id"`
}

// TableName specifies the table name for ElementTypeHierarchyQuantityGorm
func (ElementTypeHierarchyQuantityGorm) TableName() string {
	return "element_type_hierarchy_quantity"
}

// UserGorm represents the users table with GORM tags
type UserGorm struct {
	ID          uint           `gorm:"primaryKey;column:id" json:"id"`
	EmployeeId  string         `gorm:"column:employee_id;uniqueIndex" json:"employee_id"`
	Email       string         `gorm:"column:email;uniqueIndex;not null" json:"email"`
	Password    string         `gorm:"column:password;not null" json:"password"`
	FirstName   string         `gorm:"column:first_name;not null" json:"first_name"`
	LastName    string         `gorm:"column:last_name;not null" json:"last_name"`
	CreatedAt   time.Time      `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"column:updated_at;not null" json:"updated_at"`
	FirstAccess *time.Time     `gorm:"column:first_access" json:"first_access,omitempty"`
	LastAccess  *time.Time     `gorm:"column:last_access" json:"last_access,omitempty"`
	ProfilePic  string         `gorm:"column:profile_picture" json:"profile_picture"`
	IsAdmin     bool           `gorm:"column:is_admin;default:false" json:"is_admin"`
	Address     string         `gorm:"column:address" json:"address"`
	City        string         `gorm:"column:city" json:"city"`
	State       string         `gorm:"column:state" json:"state"`
	Country     string         `gorm:"column:country" json:"country"`
	ZipCode     string         `gorm:"column:zip_code" json:"zip_code"`
	PhoneNo     string         `gorm:"column:phone_no" json:"phone_no"`
	RoleID      int            `gorm:"column:role_id;not null" json:"role_id"`
	RoleName    string         `gorm:"-" json:"role_name"` // Virtual field, not stored in DB
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for UserGorm
func (UserGorm) TableName() string {
	return "users"
}

// SessionGorm represents the session table with GORM tags
type SessionGorm struct {
	ID        uint           `gorm:"primaryKey;column:id" json:"id"`
	UserID    int            `gorm:"column:user_id;not null" json:"user_id"`
	SessionID string         `gorm:"column:session_id;uniqueIndex;not null" json:"session_id"`
	HostName  string         `gorm:"column:host_name;not null" json:"host_name"`
	IPAddress string         `gorm:"column:ip_address;not null" json:"ip_address"`
	Timestamp time.Time      `gorm:"column:timestp;not null" json:"timestp"`
	ExpiresAt time.Time      `gorm:"column:expires_at;not null" json:"expires_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for SessionGorm
func (SessionGorm) TableName() string {
	return "session"
}

// ActivityLogGorm represents the activity_log table with GORM tags
type ActivityLogGorm struct {
	ID                uint           `gorm:"primaryKey;column:id" json:"id"`
	CreatedAt         time.Time      `gorm:"column:created_at;not null" json:"created_at"`
	UserName          string         `gorm:"column:user_name;not null" json:"user_name"`
	HostName          string         `gorm:"column:host_name;not null" json:"host_name"`
	EventContext      string         `gorm:"column:event_context;not null" json:"event_context"`
	IPAddress         string         `gorm:"column:ip_address;not null" json:"ip_address"`
	Description       string         `gorm:"column:description;not null" json:"description"`
	EventName         string         `gorm:"column:event_name;not null" json:"event_name"`
	AffectedUserName  string         `gorm:"column:affected_user_name" json:"affected_user_name"`
	AffectedUserEmail string         `gorm:"column:affected_user_email" json:"affected_user_email"`
	ProjectID         int            `gorm:"column:project_id" json:"project_id"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName specifies the table name for ActivityLogGorm
func (ActivityLogGorm) TableName() string {
	return "activity_log"
}

// ElementGorm represents the element table with GORM tags
type ElementGorm struct {
	ID                 uint      `gorm:"primaryKey;column:id" json:"id"`
	ElementTypeID      int       `gorm:"column:element_type_id;not null" json:"element_type_id"`
	ElementId          string    `gorm:"column:element_id;not null" json:"element_id"`
	ElementName        string    `gorm:"column:element_name;not null" json:"element_name"`
	ProjectID          int       `gorm:"column:project_id;not null" json:"project_id"`
	CreatedBy          string    `gorm:"column:created_by;not null" json:"created_by"`
	CreatedAt          time.Time `gorm:"column:created_at;not null" json:"created_at"`
	Status             string    `gorm:"column:status;default:'0'" json:"status"`
	ElementTypeVersion string    `gorm:"column:element_type_version" json:"element_type_version"`
	UpdateAt           time.Time `gorm:"column:update_at;not null" json:"update_at"`
	TargetLocation     int       `gorm:"column:target_location;not null" json:"target_location"`
	Disable            bool      `gorm:"column:disable;default:false" json:"disable"`
}

// TableName specifies the table name for ElementGorm
func (ElementGorm) TableName() string {
	return "element"
}

// DrawingsGorm represents the drawings table with GORM tags
type DrawingsGorm struct {
	DrawingsId      uint      `gorm:"primaryKey;column:drawing_id" json:"drawing_id"`
	ProjectId       int       `gorm:"column:project_id;not null" json:"project_id"`
	CurrentVersion  string    `gorm:"column:current_version;not null" json:"current_version"`
	CreatedAt       time.Time `gorm:"column:created_at;not null" json:"created_at"`
	CreatedBy       string    `gorm:"column:created_by;not null" json:"created_by"`
	DrawingTypeId   int       `gorm:"column:drawing_type_id;not null" json:"drawing_type_id"`
	DrawingTypeName string    `gorm:"-" json:"drawing_type_name"` // Virtual field
	UpdateAt        time.Time `gorm:"column:update_at;not null" json:"update_at"`
	UpdatedBy       string    `gorm:"column:updated_by;not null" json:"updated_by"`
	Comments        string    `gorm:"column:comments" json:"comments"`
	File            string    `gorm:"column:file;not null" json:"file"`
	ElementTypeID   int       `gorm:"column:element_type_id;not null" json:"element_type_id"`
}

// TableName specifies the table name for DrawingsGorm
func (DrawingsGorm) TableName() string {
	return "drawings"
}

// DrawingsRevisionGorm represents the drawings_revision table with GORM tags
type DrawingsRevisionGorm struct {
	ParentDrawingsId   uint      `gorm:"column:parent_drawing_id;not null" json:"parent_drawing_id"`
	ProjectId          int       `gorm:"column:project_id;not null" json:"project_id"`
	Version            string    `gorm:"column:version;not null" json:"version"`
	CreatedAt          time.Time `gorm:"column:created_at;not null" json:"created_at"`
	CreatedBy          string    `gorm:"column:created_by;not null" json:"created_by"`
	DrawingsTypeId     int       `gorm:"column:drawing_type_id;not null" json:"drawing_type_id"`
	DrawingTypeName    string    `gorm:"-" json:"drawing_type_name"` // Virtual field
	Comments           string    `gorm:"column:comments" json:"comments"`
	File               string    `gorm:"column:file;not null" json:"file"`
	DrawingsRevisionId uint      `gorm:"primaryKey;column:drawing_revision_id" json:"drawing_revision_id"`
	ElementTypeID      int       `gorm:"column:element_type_id;not null" json:"element_type_id"`
}

// TableName specifies the table name for DrawingsRevisionGorm
func (DrawingsRevisionGorm) TableName() string {
	return "drawings_revision"
}

// PrecastStockGorm represents the precast_stock table with GORM tags
type PrecastStockGorm struct {
	ID                uint       `gorm:"primaryKey;column:id" json:"id"`
	ElementID         int        `gorm:"column:element_id;not null" json:"element_id"`
	ElementType       string     `gorm:"column:element_type;not null" json:"element_type"`
	ElementTypeID     int        `gorm:"column:element_type_id;not null" json:"element_type_id"`
	StockyardID       int        `gorm:"column:stockyard_id;not null" json:"stockyard_id"`
	Dimensions        string     `gorm:"column:dimensions" json:"dimensions"`
	Weight            float64    `gorm:"column:weight" json:"weight"`
	Mass              float64    `gorm:"column:mass" json:"mass"`
	ProductionDate    time.Time  `gorm:"column:production_date;not null" json:"production_date"`
	StorageLocation   string     `gorm:"column:storage_location" json:"storage_location"`
	DispatchStatus    bool       `gorm:"column:dispatch_status;default:false" json:"dispatch_status"`
	CreatedAt         time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
	Stockyard         bool       `gorm:"column:stockyard;default:true" json:"stockyard"`
	ProjectID         int        `gorm:"column:project_id;not null" json:"project_id"`
	TargetLocation    int        `gorm:"column:target_location;not null" json:"target_location"`
	Disable           bool       `gorm:"column:disable;default:false" json:"disable"`
	DispatchStart     *time.Time `gorm:"column:dispatch_start" json:"dispatch_start"`
	DispatchEnd       *time.Time `gorm:"column:dispatch_end" json:"dispatch_end"`
	Erected           bool       `gorm:"column:erected;default:false" json:"erected"`
	ReceiveInErection bool       `gorm:"column:receive_in_erection;default:false" json:"receive_in_erection"`
}

// TableName specifies the table name for PrecastStockGorm
func (PrecastStockGorm) TableName() string {
	return "precast_stock"
}

// ElementTypeQuantityGorm represents the element_type_quantity table with GORM tags
type ElementTypeQuantityGorm struct {
	ID              uint   `gorm:"primaryKey;column:id" json:"id"`
	Tower           int    `gorm:"column:tower;not null" json:"tower"`
	Floor           int    `gorm:"column:floor;not null" json:"floor"`
	ElementTypeName string `gorm:"column:element_type_name;not null" json:"element_type_name"`
	ElementType     string `gorm:"column:element_type;not null" json:"element_type"`
	ElementTypeID   int    `gorm:"column:element_type_id;not null" json:"element_type_id"`
	TotalQuantity   int    `gorm:"column:total_quantity;not null" json:"total_quantity"`
	LeftQuantity    int    `gorm:"column:left_quantity;default:0" json:"left_quantity"`
	ProjectID       int    `gorm:"column:project_id;not null" json:"project_id"`
}

// TableName specifies the table name for ElementTypeQuantityGorm
func (ElementTypeQuantityGorm) TableName() string {
	return "element_type_quantity"
}

// ElementTypePathGorm represents the element_type_path table with GORM tags
type ElementTypePathGorm struct {
	ID            uint  `gorm:"primaryKey;column:id" json:"id"`
	ElementTypeID int   `gorm:"column:element_type_id;not null" json:"element_type_id"`
	StagePath     []int `gorm:"column:stage_path;type:int[]" json:"stage_path"`
}

// TableName specifies the table name for ElementTypePathGorm
func (ElementTypePathGorm) TableName() string {
	return "element_type_path"
}

// ElementTypeBomGorm represents the element_type_bom table with GORM tags
type ElementTypeBomGorm struct {
	ID            uint      `gorm:"primaryKey;column:id" json:"id"`
	ElementTypeID int       `gorm:"column:element_type_id;not null" json:"element_type_id"`
	ProjectID     int       `gorm:"column:project_id;not null" json:"project_id"`
	ProductID     int       `gorm:"column:product_id;not null" json:"product_id"`
	ProductName   string    `gorm:"column:product_name;not null" json:"product_name"`
	Quantity      float64   `gorm:"column:quantity;not null" json:"quantity"`
	Unit          string    `gorm:"column:unit" json:"unit"`
	Rate          float64   `gorm:"column:rate" json:"rate"`
	CreatedAt     time.Time `gorm:"column:created_at;not null" json:"created_at"`
	CreatedBy     string    `gorm:"column:created_by;not null" json:"created_by"`
	UpdatedAt     time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
	UpdatedBy     string    `gorm:"column:updated_by;not null" json:"updated_by"`
}

// TableName specifies the table name for ElementTypeBomGorm
func (ElementTypeBomGorm) TableName() string {
	return "element_type_bom"
}
