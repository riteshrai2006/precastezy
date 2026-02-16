package models

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

type User struct {
	ID             int       `json:"id" example:"1"`
	EmployeeId     string    `json:"employee_id" example:"EMP001"`
	Email          string    `json:"email" example:"user@example.com"`
	Password       string    `json:"password" example:""`
	FirstName      string    `json:"first_name" example:"John"`
	LastName       string    `json:"last_name" example:"Doe"`
	CreatedAt      time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt      time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	FirstAccess    time.Time `json:"first_access,omitempty" example:"2024-01-15T10:30:00Z"`
	LastAccess     time.Time `json:"last_access,omitempty" example:"2024-01-15T10:30:00Z"`
	ProfilePic     string    `json:"profile_picture" example:""`
	IsAdmin        bool      `json:"is_admin" example:"false"`
	Address        string    `json:"address" example:"123 Main St"`
	City           string    `json:"city" example:"Mumbai"`
	State          string    `json:"state" example:"Maharashtra"`
	Country        string    `json:"country" example:"India"`
	ZipCode        string    `json:"zip_code" example:"400001"`
	PhoneNo        string    `json:"phone_no" example:"9876543210"`
	RoleID         int       `json:"role_id" example:"1"`
	RoleName       string    `json:"role_name" example:"Manager"`
	Suspended      bool      `json:"suspended" example:"false"`
	ProjectSuspend bool      `json:"project_suspend" example:"false"`
	PhoneCode      int       `json:"phone_code" example:"91"`
	PhoneCodeName  string    `json:"phone_code_name,omitempty" example:"+91"`
}

// InvPurchase represents the inv_purchase table.
type InvPurchase struct {
	PurchaseID    int           `json:"purchase_id" example:"1"`
	Description   string        `json:"description" example:"Cement purchase"`
	ProjectID     int           `json:"project_id" example:"1"`
	VendorID      int           `json:"vendor_id" example:"1"`
	WarehouseID   int           `json:"warehouse_id" example:"1"`
	PurchaseDate  time.Time     `json:"purchase_date" example:"2024-01-15T00:00:00Z"`
	DeliveredDate time.Time     `json:"delivered_date" example:"2024-01-20T00:00:00Z"`
	SubTotal      float64       `json:"sub_total" example:"10000.50"`
	Tax           float64       `json:"tax" example:"1800.09"`
	TotalCost     float64       `json:"total_cost" example:"11800.59"`
	PaymentMode   string        `json:"payment_mode" example:"bank_transfer"`
	Status        string        `json:"status" example:"delivered"`
	CustomerNote  string        `json:"customer_note" example:"Handle with care"`
	Timedatestamp time.Time     `json:"timedatestamp" example:"2024-01-15T10:30:00Z"`
	UpdatedBy     string        `json:"updated_by" example:"admin"`
	CreatedBy     string        `json:"created_by" example:"admin"`
	PurchaseBOM   []PurchaseBOM `json:"purchase_bom"`
	VendorName    string        `json:"vendor_name,omitempty" example:"ABC Suppliers"`
	WarehouseName string        `json:"warehouse_name,omitempty" example:"Main Warehouse"`
}
type PurchaseBOM struct {
	BomID  int     `json:"bom_id" example:"1"`
	BomQty float64 `json:"bom_qty" example:"10.5"`
}

// InvLineItem represents the inv_line_items table.
type InvLineItem struct {
	ItemsID    int     `json:"items_id" example:"1"`
	PurchaseID int     `json:"purchase_id" example:"1"`
	BomID      int     `json:"bom_id" example:"1"`
	BomQty     float64 `json:"bom_qty" example:"5.5"`
	BomRate    float64 `json:"bom_rate" example:"100.00"`
	SubTotal   float64 `json:"sub_total" example:"550.00"`
	BomName    string  `json:"bom_name,omitempty" example:"Cement"`
}

// InvTransaction represents the inv_transaction table.
type InvTransaction struct {
	TransactionID int       `json:"inv_transaction_id" example:"1"`
	PurchaseID    int       `json:"purchase_id" example:"1"`
	WarehouseID   int       `json:"warehouse_id" example:"1"`
	ProjectID     int       `json:"project_id" example:"1"`
	TaskID        int       `json:"task_id" example:"1"`
	BomID         int       `json:"bom_id" example:"1"`
	BomQty        float64   `json:"bom_qty" example:"2.0"`
	Status        string    `json:"status" example:"in"`
	TimeDate      time.Time `json:"time_date" example:"2024-01-15T10:30:00Z"`
	BomName       string    `json:"bom_name,omitempty" example:"Cement"`
	WarehouseName string    `json:"warehouse_name,omitempty" example:"Main Warehouse"`
}

// InvTrack represents the inv_track table.
type InvTrack struct {
	TrackID              int       `json:"inv_track_id" example:"1"`
	ProjectID            int       `json:"project_id" example:"1"`
	BomID                int       `json:"bom_id" example:"1"`
	BomQty               float64   `json:"bom_qty" example:"50.0"`
	WarehouseID          int       `json:"warehouse_id" example:"1"`
	LastUpdated          time.Time `json:"last_updated" example:"2024-01-15T10:30:00Z"`
	LastInvTransactionID int       `json:"last_inv_transactionID" example:"1"`
}

type Warehouse struct {
	ID            int       `json:"id" example:"1"`
	Name          string    `json:"name" example:"Main Warehouse"`
	Location      string    `json:"location" example:"Site A"`
	ContactNumber string    `json:"contact_number" example:"9876543210"`
	Email         string    `json:"email" example:"warehouse@example.com"`
	Capacity      int       `json:"capacity" example:"1000"`
	UsedCapacity  int       `json:"used_capacity" example:"200"`
	Description   string    `json:"description" example:"Primary storage"`
	CreatedAt     time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt     time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	ProjectID     int       `json:"project_id" example:"1"`
	ProjectName   string    `json:"project_name,omitempty" example:"Project Alpha"`
}

// Vendor represents the structure for the vendors table.
type Vendor struct {
	VendorID   int       `json:"vendor_id" example:"1"`
	Name       string    `json:"name" example:"ABC Suppliers"`
	Email      string    `json:"email" example:"vendor@example.com"`
	Phone      string    `json:"phone" example:"9876543210"`
	Address    string    `json:"address" example:"123 Industrial Area"`
	Status     string    `json:"status" example:"active"`
	VendorType string    `json:"vendor_type" example:"material"`
	CreatedAt  time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt  time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	CreatedBy  string    `json:"created_by" example:"admin"`
	UpdatedBy  string    `json:"updated_by" example:"admin"`
	ProjectID  int       `json:"project_id" example:"1"`
}

type ElementTypeRevision struct {
	ElementTypeRevisionID int    `json:"element_type_revision_id" example:"1"`
	ElementTypeID         int    `json:"element_type_id" example:"1"`
	ElementType           string `json:"element_type" example:"B1"`
	ElementTypeName       string `json:"element_type_name" example:"Beam Type 1"`
	Thickness             float64 `json:"thickness" example:"150.0"`
	Length                float64 `json:"length" example:"3000.0"`
	Height                float64 `json:"height" example:"400.0"`
	Volume                float64 `json:"volume" example:"0.18"`
	Mass                  float64 `json:"mass" example:"450.0"`
	Area                  float64 `json:"area" example:"1.2"`
	Width                 float64             `json:"width" example:"200.0"`
	CreatedBy             string              `json:"created_by" example:"admin"`
	CreatedAt             time.Time           `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt             time.Time           `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	ProjectID             int                 `json:"project_id" example:"1"`
	ElementTypeVersion    string              `json:"element_type_version" example:"v1"`
	TotalCountElement     int                 `json:"total_count_element" example:"10"`
	HierarchyQ            []HierarchyQuantity `json:"hierarchy_quantity"`
}

type ElementTypename struct {
	ID              int    `json:"id" example:"1"`
	ElementTypeName string `json:"element_type" example:"B1"`
	ProjectID       int    `json:"project_id" example:"1"`
}
type Element struct {
	Id                 int       `json:"id" example:"1"`
	ElementTypeID      int       `json:"element_type_id" example:"1"`
	ElementId          string    `json:"element_id" example:"B1-001"`
	ElementName        string    `json:"element_name" example:"Beam 1"`
	ProjectID          int       `json:"project_id" example:"1"`
	CreatedBy          string    `json:"created_by" example:"admin"`
	CreatedAt          time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	Status             int       `json:"status" example:"1"`
	ElementTypeVersion string    `json:"element_type_version" example:"v1"`
	UpdateAt           time.Time `json:"update_at" example:"2024-01-15T10:30:00Z"`
	TargetLocation     int       `json:"target_location" example:"1"`
	Disable            bool      `json:"disable" example:"false"`
}

type ElementType struct {
	ElementType        string              `json:"element_type" example:"B1"`
	ElementTypeName    string              `json:"element_type_name" example:"Beam Type 1"`
	Thickness          float64             `json:"thickness" example:"150.0"`
	Length             float64             `json:"length" example:"3000.0"`
	Height             float64             `json:"height" example:"400.0"`
	Volume             float64             `json:"volume" example:"0.18"`
	Mass               float64             `json:"mass" example:"450.0"`
	Area               float64             `json:"area" example:"1.2"`
	Width              float64             `json:"width" example:"200.0"`
	CreatedBy          string              `json:"created_by" example:"admin"`
	CreatedAt          time.Time           `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt          time.Time           `json:"update_at" example:"2024-01-15T10:30:00Z"`
	ElementTypeId      int                 `json:"element_type_id" example:"1"`
	ProjectID          int                 `json:"project_id" example:"1"`
	SessionID          string              `json:"session_id" example:""`
	ElementTypeVersion string              `json:"element_type_version" example:"v1"`
	TotalCountElement  int                 `json:"total_count_element" example:"10"`
	Density            float64             `json:"density" example:"2500"`
	HierarchyQ         []HierarchyQuantity `json:"hierarchy_quantity"`
	Stage              []Stages            `json:"stages"`
	Drawings           []Drawings          `json:"drawings"`
	Products           []Product           `json:"products"`
}

type Stages struct {
	Element_type_id int64 `json:"element_type_id" example:"1"`
	Order           int   `json:"order" example:"1"`
	StagePath       []int `json:"stage_path" example:"1,2,3"`
	StagesID        int   `json:"stage_id" example:"1"`
}

type HierarchyQuantity struct {
	HierarchyId      int    `json:"hierarchy_id" example:"1"`
	Quantity         int    `json:"quantity" example:"5"`
	NamingConvention string `json:"naming_convention" example:"T1-F1"`
}
type Drawings struct {
	DrawingsId       int                `json:"drawing_id" example:"1"`
	ProjectId        int                `json:"project_id" example:"1"`
	CurrentVersion   string             `json:"current_version" example:"v1"`
	CreatedAt        time.Time          `json:"created_at" example:"2024-01-15T10:30:00Z"`
	CreatedBy        string             `json:"created_by" example:"admin"`
	DrawingTypeId    int                `json:"drawing_type_id" example:"1"`
	DrawingTypeName  string             `json:"drawing_type_name" example:"Plan"`
	UpdateAt         time.Time          `json:"update_at" example:"2024-01-15T10:30:00Z"`
	UpdatedBy        string             `json:"updated_by" example:"admin"`
	Comments         string             `json:"comments" example:""`
	File             string             `json:"file" example:"/files/drawing.pdf"`
	ElementTypeID    int                `json:"Element_type_id" example:"1"`
	DrawingsRevision []DrawingsRevision `json:"drawingsRevision"`
}
type DrawingsRevision struct {
	ParentDrawingsId   int       `json:"parent_drawing_id" example:"1"`
	ProjectId          int       `json:"project_id" example:"1"`
	Version            string    `json:"version" example:"v1"`
	CreatedAt          time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	CreatedBy          string    `json:"created_by" example:"admin"`
	DrawingsTypeId     int       `json:"drawing_type_id" example:"1"`
	DrawingTypeName    string    `json:"drawing_type_name" example:"Plan"`
	Comments           string    `json:"comments" example:""`
	File               string    `json:"file" example:"/files/rev.pdf"`
	DrawingsRevisionId int       `json:"drawing_revision_id" example:"1"`
	ElementTypeID      int       `json:"Element_type_id" example:"1"`
}

type DrawingWithRevisions struct {
	ID          int                `json:"id" example:"1"`
	DrawingType string             `json:"drawing_type" example:"Plan"`
	DrawingName string             `json:"drawing_name" example:"Floor Plan"`
	Revisions   []DrawingsRevision `json:"revisions"`
}

type Product struct {
	ProductID   int     `json:"product_id" example:"1"`
	ProductName string  `json:"product_name" example:"Cement"`
	Quantity    float64 `json:"quantity" example:"2.5"`
	Unit        string  `json:"unit,omitempty" example:"Cum"`
	Rate        float64 `json:"rate,omitempty" example:"5000.00"`
}

type BOMPro struct {
	ID            int       `json:"id" example:"1"`
	ElementTypeID int       `json:"element_type_id" example:"1"`
	ProjectId     int       `json:"project_id" example:"1"`
	CreatedAt     time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	CreatedBy     string    `json:"created_by" example:"admin"`
	UpdatedAt     time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	UpdatedBy     string    `json:"updated_by" example:"admin"`
	Product       string    `json:"product" example:"Cement, Steel"`
}

type BOMRevision struct {
	BOMRevisionID int       `json:"bom_revision_id" example:"1"`
	BOMProID      int       `json:"bompro_id" example:"1"`
	ElementTypeID int       `json:"element_type_id" example:"1"`
	ProjectID     int       `json:"project_id" example:"1"`
	CreatedAt     time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	CreatedBy     string    `json:"created_by" example:"admin"`
	Product       []Product `json:"product"`
}
type DrawingsType struct {
	DrawingsTypeId  int    `json:"drawings_type_id" example:"1"`
	DrawingTypeName string `json:"drawing_type_name" example:"Plan"`
	ProjectId       int    `json:"project_id" example:"1"`
}

type DateOnly struct {
	time.Time
}

const dateFormat = "2006-01-02"

func (d *DateOnly) UnmarshalJSON(data []byte) error {
	parsedTime, err := time.Parse(`"`+dateFormat+`"`, string(data))
	if err != nil {
		return err
	}
	d.Time = parsedTime
	return nil
}

func (d DateOnly) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.Time.Format(dateFormat))
}

func (d DateOnly) ToTime() time.Time {
	return d.Time
}

// Scan implements the Scanner interface for DateOnly type
func (d *DateOnly) Scan(value interface{}) error {
	if value == nil {
		d.Time = time.Time{} // Set to zero time if the value is NULL
		return nil
	}
	switch v := value.(type) {
	case time.Time:
		d.Time = v
		// Set the time to midnight (00:00:00) to store only the date
		d.Time = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, d.Location())
		return nil
	default:
		return fmt.Errorf("cannot scan type %T into DateOnly", v)
	}
}

// Value implements driver.Valuer for database/sql
func (d DateOnly) Value() (driver.Value, error) {
	return d.Time, nil
}

type Project struct {
	ProjectId             int            `json:"project_id" example:"1"`
	Name                  string         `json:"name" example:"Project Alpha"`
	Priority              string         `json:"priority" example:"high"`
	ProjectStatus         string         `json:"project_status" example:"active"`
	StartDate             DateOnly       `json:"start_date" example:"2024-01-01"`
	EndDate               DateOnly       `json:"end_date" example:"2024-12-31"`
	Logo                  string         `json:"logo" example:""`
	Description           string         `json:"description" example:"Precast building project"`
	CreatedAt             time.Time      `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt             time.Time      `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	LastUpdated           time.Time      `json:"last_updated" example:"2024-01-15T10:30:00Z"`
	LastUpdatedBy         string         `json:"last_updated_by" example:"admin"`
	ClientId              int            `json:"client_id" example:"1"`
	Budget                string         `json:"budget" example:"1000000"`
	Users                 []int          `json:"users"`
	TemplateID            int            `json:"template_id" example:"1"`
	SubscriptionStartDate DateOnly       `json:"subscription_start_date" example:"2024-01-01"`
	SubscriptionEndDate   DateOnly       `json:"subscription_end_date" example:"2024-12-31"`
	Stockyards            []int          `json:"stockyards"`
	Roles                 []RoleQuantity `json:"roles"`
	Abbreviation          string         `json:"abbreviation" example:"PA"`
	WorkOrder             bool           `json:"work_order" example:"true"`
	HRA                   bool           `json:"hra" example:"false"`
	Invoice               bool           `json:"invoice" example:"true"`
	Calculator            bool           `json:"calculator" example:"false"`
}

type ResponseProject struct {
	ProjectId             int         `json:"project_id" example:"1"`
	Name                  string      `json:"name" example:"Project Alpha"`
	Priority              string      `json:"priority" example:"high"`
	ProjectStatus         string      `json:"project_status" example:"active"`
	StartDate             DateOnly    `json:"start_date" example:"2024-01-01"`
	EndDate               DateOnly    `json:"end_date" example:"2024-12-31"`
	Logo                  string      `json:"logo" example:""`
	Description           string      `json:"description" example:"Precast building project"`
	CreatedAt             time.Time   `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt             time.Time   `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	LastUpdated           time.Time   `json:"last_updated" example:"2024-01-15T10:30:00Z"`
	LastUpdatedBy         string      `json:"last_updated_by" example:"admin"`
	ClientId              int         `json:"client_id" example:"1"`
	Budget                string      `json:"budget" example:"1000000"`
	TemplateID            int         `json:"template_id" example:"1"`
	SubscriptionStartDate DateOnly    `json:"subscription_start_date" example:"2024-01-01"`
	SubscriptionEndDate   DateOnly    `json:"subscription_end_date" example:"2024-12-31"`
	Stockyards            []Stockyard `json:"stockyards"`
	Abbreviation          string      `json:"abbreviation" example:"PA"`
}

type StockyardMinimal struct {
	ID   int    `json:"id" example:"1"`
	Name string `json:"name" example:"Yard A"`
}

type ProjectMetricsWithDetails struct {
	Projects
	ProjectID           int                `json:"project_id" example:"1"`
	TotalElements       int                `json:"total_elements" example:"100"`
	ErectedElements     int                `json:"erected_elements" example:"50"`
	CastedElements      int                `json:"casted_elements" example:"60"`
	InStock             int                `json:"in_stock" example:"20"`
	InProduction        int                `json:"in_production" example:"10"`
	ElementTypeCount    int                `json:"element_type_count" example:"5"`
	ProjectMembersCount int                `json:"project_members_count" example:"8"`
	Stockyards          []StockyardMinimal `json:"stockyards"`
}

type Projects struct {
	ProjectId             int       `json:"project_id" example:"1"`
	Name                  string    `json:"name" example:"Project Alpha"`
	Priority              string    `json:"priority" example:"high"`
	ProjectStatus         string    `json:"project_status" example:"active"`
	StartDate             DateOnly  `json:"start_date" example:"2024-01-01"`
	EndDate               DateOnly  `json:"end_date" example:"2024-12-31"`
	Logo                  string    `json:"logo" example:""`
	Description           string    `json:"description" example:"Precast building project"`
	CreatedAt             time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt             time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	LastUpdated           time.Time `json:"last_updated" example:"2024-01-15T10:30:00Z"`
	LastUpdatedBy         string    `json:"last_updated_by" example:"admin"`
	ClientId              int       `json:"client_id" example:"1"`
	Budget                string    `json:"budget" example:"1000000"`
	Suspend               bool      `json:"suspend" example:"false"`
	TemplateID            int       `json:"template_id" example:"1"`
	SubscriptionStartDate DateOnly `json:"subscription_start_date" example:"2024-01-01"`
	SubscriptionEndDate   DateOnly `json:"subscription_end_date" example:"2024-12-31"`
	Stockyards            []int    `json:"stockyards"`
}

type RoleQuantity struct {
	RoleID   int    `json:"role_id" example:"1"`
	RoleName string `json:"role_name" example:"Manager"`
	Quantity int    `json:"quantity" example:"2"`
}

type ProjectStockyard struct {
	ID          int       `json:"id" example:"1"`
	ProjectID   int       `json:"project_id" example:"1"`
	StockyardID int       `json:"stockyard_id" example:"1"`
	UserID      *int      `json:"user_id"`
	CreatedAt   time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt   time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
}

type ProjectStockyardDetail struct {
	ID          int       `json:"id" example:"1"`
	ProjectID   int       `json:"project_id" example:"1"`
	StockyardID int       `json:"stockyard_id" example:"1"`
	UserID      *int      `json:"user_id"`
	CreatedAt   time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt   time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	ProjectName string    `json:"project_name" example:"Project Alpha"`
	YardName    string    `json:"yard_name" example:"Yard A"`
	UserName    string    `json:"user_name" example:"John Doe"`
}
type QCStatus struct {
	ElementID      int       `json:"element_id" example:"1"`
	ProjectID      int       `json:"project_id" example:"1"`
	DesignID       int       `json:"design_id" example:"1"`
	QCStatus       string    `json:"qc_status" example:"Passed"`
	Remarks        string    `json:"remarks" example:""`
	Status         string    `json:"status" example:"completed"`
	PictureGallery string    `json:"picture_gallery" example:""`
	DateCreated    time.Time `json:"date_created" example:"2024-01-15T10:30:00Z"`
	DateAssigned   time.Time `json:"date_assingned" example:"2024-01-15T10:30:00Z"`
	CompletionDate time.Time `json:"completion_date" example:"2024-01-15T10:30:00Z"`
	DateCompleted  time.Time `json:"date_completed" example:"2024-01-15T10:30:00Z"`
}

type Client struct {
	ClientID     int    `json:"client_id" example:"1"`
	UserID       int    `json:"user_id" example:"1"`
	Organization string `json:"organization" example:"Acme Corp"`
	StoreID      int    `json:"store_id" example:"1"`
}

type ElementInput struct {
	ElementTypeID      int    `json:"element_type_id" example:"1"`
	Quantity           int    `json:"quantity" example:"5"`
	SessionID          string `json:"session_id" example:""`
	NamingConvention   string `json:"naming_convention" example:"T1-F1"`
	HierarchyId        int    `json:"hierarchy_id" example:"1"`
	ProjectID          int    `json:"project_id" example:"1"`
	ElementType        string `json:"element_type" example:"B1"`
	ElementTypeName    string `json:"element_type_name" example:"Beam Type 1"`
	ElementTypeVersion string `json:"element_type_version" example:"v1"`
	TotalCountElement  int    `json:"total_count_element" example:"10"`
}

type Session struct {
	UserID                int       `json:"user_id"`
	SessionID             string    `json:"session_id"`
	HostName              string    `json:"host_name"`
	IPAddress             string    `json:"ip_address"`
	Timestamp             time.Time `json:"timestp"`
	ExpiresAt             time.Time `json:"expires_at"`
	RefreshToken          string    `json:"refresh_token,omitempty"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at,omitempty"`
}

func GetSessionBySessionID(db *sql.DB, sessionID string) (*Session, error) {
	query := `SELECT session_id, user_id, host_name, timestp FROM session WHERE session_id = $1`

	var session Session

	err := db.QueryRow(query, sessionID).Scan(&session.SessionID, &session.UserID, &session.HostName, &session.Timestamp)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("session not found")
		}
		return nil, err
	}

	return &session, nil
}

// BOM represents a Bill of Materials.----------------------------------
// BOMProduct is the request body for update_bom_products (PUT /api/update_bom_products/:id).
// Required for update: bom_name, bom_type, unit, project_id.
type BOMProduct struct {
	ID          int       `json:"bom_id,omitempty"`
	ProductName string    `json:"bom_name" example:"Aggregate"`
	ProductType string    `json:"bom_type" example:"200MM"`
	Unit        string    `json:"unit" example:"Cum"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
	ProjectId   int       `json:"project_id" example:"731623920"`

	NameId      string  `json:"name_id,omitempty"`
	Vendor      *string `json:"vendor,omitempty"`
	MasterBomId *int    `json:"master_bom_id,omitempty"`
}

type BOM struct {
	ID            int       `json:"id" example:"1"`
	ElementTypeID int       `json:"element_type_id" example:"1"`
	ProjectID     int       `json:"project_id" example:"1"`
	CreatedAt     time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt     time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
}

type BOMQty struct {
	ID           int       `json:"id" example:"1"`
	BomID        int       `json:"bom_id" example:"1"`
	BOMProductID int       `json:"bom_product_id" example:"1"`
	Quantity     int       `json:"quantity" example:"2"`
	CreatedAt    time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt    time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
}

type BOMWithProducts struct {
	BOM         BOM      `json:"bom"`
	BOMProducts []BOMQty `json:"bom_products"`
}

// KANBAN ---------------------------------------------------------------------------------------------------------

// AssignedTo represents stage assignment with members.
type AssignedTo struct {
	StageID   int    `json:"stage_id" example:"1"`
	MemberIDs []int  `json:"member_id" example:"1,2"`
	Members   []User `json:"members"`
	User      User   `json:"user"`
}

// MoveTaskInput is used to move a task to another stage.
type MoveTaskInput struct {
	TaskID  int `json:"task_id" example:"1"`
	StageID int `json:"stage_id" example:"2"`
}

type Task struct {
	TaskID               int        `json:"task_id" example:"1"`
	ProjectID            int        `json:"project_id" example:"1"`
	TaskTypeId           int        `json:"task_type_id" example:"1"`
	Name                 string     `json:"name" example:"Cast Beam B1"`
	StageID              int        `json:"stage_id" example:"1"`
	Desc                 string     `json:"description" example:"Casting task"`
	Priority             string     `json:"priority" example:"high"`
	FileAttachments      []string   `json:"file_attachments"`
	AssignedTo           int        `json:"assigned_to" example:"1"`
	EstimatedEffortInHrs int        `json:"estimated_effort_in_hrs" example:"4"`
	StartDate            DateOnly   `json:"start_date" example:"2024-01-15"`
	EndDate              DateOnly   `json:"end_date" example:"2024-01-20"`
	Status               string     `json:"status" example:"in_progress"`
	ColorCode            string     `json:"color_code" example:"#3498db"`
	ElementTypeID        int        `json:"element_type_id" example:"1"`
	FloorID              int        `json:"floor_id" example:"1"`
	Quantity             int        `json:"quantity" example:"5"`
	Activities           []Activity `json:"activities"`
	ElementType          string     `json:"element_type" example:"B1"`
}

type Activity struct {
	ID                    int       `json:"id" example:"1"`
	TaskID                int       `json:"task_id" example:"1"`
	ProjectID             int       `json:"project_id" example:"1"`
	Name                  string    `json:"name" example:"Mesh placement"`
	StageID               int       `json:"stage_id" example:"1"`
	PaperID               int       `json:"paper_id" example:"1"`
	Priority              string    `json:"priority" example:"high"`
	AssignedTo            int       `json:"assigned_to" example:"1"`
	StartDate             time.Time `json:"start_date" example:"2024-01-15T10:30:00Z"`
	EndDate               time.Time `json:"end_date" example:"2024-01-15T18:30:00Z"`
	Status                string    `json:"status" example:"completed"`
	ElementID             int       `json:"element_id" example:"1"`
	QCID                  int       `json:"qc_id" example:"1"`
	QCStatus              string    `json:"qc_status" example:"Passed"`
	StageName             string    `json:"stage_name" example:"Casting"`
	Stages                []string  `json:"stages"`
	MeshMoldStatus        string    `json:"mesh_mold_status" example:"done"`
	ReinforcementStatus   string    `json:"reinforcement_status" example:"done"`
	StockyardID           int       `json:"stockyard_id" example:"1"`
	MeshMoldQCStatus      string    `json:"mesh_mold_qc_status" example:"Passed"`
	ReinforcementQCStatus string    `json:"reinforcement_qc_status" example:"Passed"`
	ElementTypeID         int       `json:"element_type_id" example:"1"`
}

type CompleteProduction struct {
	ID            int        `json:"id" example:"1"`
	TaskID        int        `json:"task_id" example:"1"`
	ActivityID    int        `json:"activity_id" example:"1"`
	ProjectID     int        `json:"project_id" example:"1"`
	ElementID     int        `json:"element_id" example:"1"`
	ElementTypeID int        `json:"element_type_id" example:"1"`
	UserID        int        `json:"user_id" example:"1"`
	StageID       int        `json:"stage_id" example:"1"`
	StartedAt     time.Time  `json:"started_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
	Status        string     `json:"status,omitempty" example:"completed"`
	FloorID       int        `json:"floor_id" example:"1"`
}

type CompleteProductionResponse struct {
	ID                 int    `json:"id" example:"1"`
	TaskID             int    `json:"task_id" example:"1"`
	ActivityID         int    `json:"activity_id" example:"1"`
	ProjectID          int    `json:"project_id" example:"1"`
	ElementID          int    `json:"element_id" example:"1"`
	ElementName        string `json:"element_name" example:"Beam 1"`
	ElementTypeID      int    `json:"element_type_id" example:"1"`
	ElementType        string `json:"element_type" example:"B1"`
	ElementTypeName    string `json:"element_type_name" example:"Beam Type 1"`
	ElementTypeVersion string `json:"element_type_version" example:"v1"`
	UserID             int    `json:"user_id" example:"1"`
	StageID            int    `json:"stage_id" example:"1"`
	StageName          string `json:"stage_name" example:"Casting"`
	StartedAt          string `json:"started_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt          string `json:"updated_at" example:"2024-01-15T18:30:00Z"`
	Status             string `json:"status" example:"completed"`
	FloorID            int    `json:"floor_id" example:"1"`
	TowerID            int    `json:"tower_id" example:"1"`
	TowerName          string `json:"tower_name" example:"Tower A"`
	FloorName          string `json:"floor_name" example:"Floor 1"`
}

// ProjectStages holds stage configuration for a project.
type ProjectStages struct {
	ID                 int    `json:"id" example:"1"`
	Name               string `json:"name" example:"Casting"`
	ProductionQuantity int    `json:"production_quantity" example:"5"`
	QCQuantity         int    `json:"qc_quantity" example:"5"`
	ProjectID          int    `json:"project_id" example:"1"`
	AssignedTo         int    `json:"assigned_to" example:"1"`
	QCAssign           bool   `json:"qc_assign" example:"true"`
	QCID               int    `json:"qc_id" example:"1"`
	PaperID            int    `json:"paper_id" example:"1"`
	TemplateID         int    `json:"template_id" example:"1"`
	Order              int    `json:"order" example:"1"`
	CompletionStage    bool   `json:"completion_stage" example:"false"`
	InventoryDeduction bool   `json:"inventory_deduction" example:"true"`
	Status             string `json:"status" example:"active"`
	QCStatus           string `json:"qc_status" example:"pending"`
	Editable           string `json:"editable" example:"true"`
	QCEditable         string `json:"qc_editable" example:"true"`
	Quantity           int    `json:"quantity" example:"5"`
}

// ElementTypePath stores stage path for an element type.
type ElementTypePath struct {
	ElementTypeID int   `json:"element_type_id" example:"1"`
	StagePath     []int `json:"stage_path" example:"1,2,3"`
}

// MoveActivityInput is used to move an activity to another stage.
type MoveActivityInput struct {
	ActivityID int `json:"id" example:"1"`
	StageID    int `json:"stage_id" example:"2"`
}

// TaskType represents a type of task (Task/Change/Bug etc.).
type TaskType struct {
	ID        int    `json:"id" example:"1"`
	ProjectID int    `json:"project_id" example:"1"`
	Name      string `json:"name" example:"Task"`
	ColorCode string `json:"color_code" example:"#3498db"`
}

// Milestone represents a project milestone.
type Milestone struct {
	ID                 int       `json:"id" example:"1"`
	ProjectID          int       `json:"project_id" example:"1"`
	MilestoneName      string    `json:"milestone_name" example:"Foundation Complete"`
	MilestoneStartDate time.Time `json:"milestone_start_date" example:"2024-01-01T00:00:00Z"`
	MilestoneEndDate   time.Time `json:"milestone_end_date" example:"2024-01-31T00:00:00Z"`
	Status             string    `json:"status" example:"In Progress"`
}

// Timesheet represents hours logged on a task/activity.
type Timesheet struct {
	ID         int     `json:"id" example:"1"`
	ProjectID  int     `json:"project_id" example:"1"`
	TaskID     int     `json:"task_id" example:"1"`
	Type       string  `json:"type" example:"Task"`
	AssigneeID int     `json:"assignee_id" example:"1"`
	Hours      float64 `json:"hours" example:"4.5"`
}

type TaskWithActivities struct {
	Task       Task       `json:"task"`
	Activities []Activity `json:"activities"`
}

// Warehouse

type ActivityElement struct {
	ID         int `json:"id"`
	ActivityID int `json:"activity_id"`
	ElementID  int `json:"element_id"`
}

// RBAC ------------------------------------------------------------------------------
// -----------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------

type ProjectRoles struct {
	RoleID    int `json:"role_id" example:"1"`
	ProjectID int `json:"project_id" example:"1"`
}

type Role struct {
	RoleID   int    `json:"role_id" example:"1"`
	RoleName string `json:"role_name" example:"Manager"`
}

type Permission struct {
	PermissionID   int    `json:"permission_id" example:"1"`
	PermissionName string `json:"permission_name" example:"view_project"`
}

type RolePermission struct {
	RoleID       int `json:"role_id" example:"1"`
	PermissionID int `json:"permission_id" example:"1"`
}

type ProjectMember struct {
	ID        int       `json:"id" example:"1"`
	ProjectID int       `json:"project_id" example:"1"`
	UserID    int       `json:"user_id" example:"1"`
	RoleID    int       `json:"role_id" example:"1"`
	CreatedAt time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
}

type Notification struct {
	ID        int       `json:"id" example:"1"`
	UserID    int       `json:"user_id" example:"1"`
	Message   string    `json:"message" example:"Task assigned to you"`
	Status    string    `json:"status" example:"unread"`
	Action    string    `json:"action" example:"view"`
	CreatedAt time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
}

type Paper struct {
	ID        int    `json:"id" example:"1"`
	Name      string `json:"name" example:"QC Paper 1"`
	ProjectID int    `json:"project_id" example:"1"`
}

type Question struct {
	ID           int       `json:"id" example:"1"`
	ProjectID    int       `json:"project_id" example:"1"`
	PaperID      int       `json:"paper_id" example:"1"`
	QuestionText string    `json:"question_text" example:"Is surface finish acceptable?"`
	CreatedAt    time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	Options      []Option  `json:"options"`
}

// Option represents a possible answer for a question.
type Option struct {
	ID         int    `json:"id" example:"1"`
	QuestionID int    `json:"question_id" example:"1"`
	OptionText string `json:"option_text" example:"Yes"`
}

// QCAnswer represents an answer submitted by QC.
type QCAnswer struct {
	ID         int       `json:"id" example:"1"`
	ProjectID  int       `json:"project_id" example:"1"`
	QCID       int       `json:"qc_id" example:"1"`
	QuestionID int       `json:"question_id" example:"1"`
	OptionID   *int      `json:"option_id"`
	TaskID     int       `json:"task_id" example:"1"`
	StageID    int       `json:"stage_id" example:"1"`
	Comment    *string   `json:"comment"`
	ImagePath  *string   `json:"image_path"`
	CreatedAt  time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt  time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	ElementID  int       `json:"element_id" example:"1"`
}

type Setting struct {
	UserID                int  `json:"user_id" example:"1"`
	AllowMultipleSessions bool `json:"allow_multiple_sessions" example:"true"`
}

type ActivityLog struct {
	ID                int       `json:"id" example:"1"`
	CreatedAt         time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UserName          string    `json:"user_name" example:"John Doe"`
	HostName          string    `json:"host_name" example:"workstation-01"`
	EventContext      string    `json:"event_context" example:"project"`
	IPAddress         string    `json:"ip_address" example:"192.168.1.1"`
	Description       string    `json:"description" example:"User logged in"`
	EventName         string    `json:"event_name" example:"login"`
	AffectedUserName  string    `json:"affected_user_name" example:"Jane Doe"`
	AffectedUserEmail string    `json:"affected_user_email" example:"jane@example.com"`
	ProjectID         int       `json:"project_id" example:"1"`
}

type Template struct {
	ID     int     `json:"id" example:"1"`
	Name   string  `json:"name" example:"Standard Precast"`
	Stages []Stage `json:"stages"`
}

// Stage is a template stage (production/QC stage).
type Stage struct {
	ID                 int    `json:"id" example:"1"`
	Name               string `json:"name" example:"Casting"`
	QCAssign           bool   `json:"qc_assign" example:"true"`
	TemplateID         int    `json:"template_id,omitempty" example:"1"`
	Order              int    `json:"order" example:"1"`
	CompletionStage    bool   `json:"completion_stage" example:"false"`
	InventoryDeduction bool   `json:"inventory_deduction" example:"true"`
}

// ElementType represents an element type with only ID and Name
type ElementTypeVersion struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ProjectID int    `json:"project_id"`
}

type ResponseTask struct {
	TaskID        int        `json:"task_id"`
	ProjectID     int        `json:"project_id"`
	ElementTypeID int        `json:"element_type_id"`
	ElementType   string     `json:"element_type"`
	FloorID       int        `json:"floor_id"`
	Activities    []Activity `json:"activities"` // Ensure Activities is a slice of Activity
}
type PrecastStock struct {
	ID              int       `json:"id"`
	ElementID       int       `json:"element_id"`
	ElementType     string    `json:"element_type"`
	ElementTypeID   int       `json:"element_type_id"`
	StockyardID     int       `json:"stockyard_id"`
	Dimensions      string    `json:"dimensions"`
	Thickness       string    `json:"thickness"`
	Length          string    `json:"length"`
	Height          string    `json:"height"`
	Weight          float64   `json:"weight"`
	Mass            float64   `json:"mass"`
	ProductionDate  time.Time `json:"production_date"`
	StorageLocation string    `json:"storage_location"`
	DispatchStatus  bool      `json:"dispatch_status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Stockyard       bool      `json:"stockyard"`
	ProjectID       int       `json:"project_id"`
	TargetLocation  int       `json:"target_location"`
	Disable         bool      `json:"disable"`
}

type PrecastStock2 struct {
	ID                int       `json:"id"`
	ElementID         int       `json:"element_id"`
	ElementType       string    `json:"element_type"`
	ElementTypeID     int       `json:"element_type_id"`
	StockyardID       int       `json:"stockyard_id"`
	Dimensions        string    `json:"dimensions"`
	Weight            float64   `json:"weight"`
	Mass              float64   `json:"mass"`
	ProductionDate    time.Time `json:"production_date"`
	StorageLocation   string    `json:"storage_location"`
	DispatchStatus    bool      `json:"dispatch_status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Stockyard         bool      `json:"stockyard"`
	ProjectID         int       `json:"project_id"`
	TargetLocation    int       `json:"target_location"`
	Disable           bool      `json:"disable"`
	DispatchStart     time.Time `json:"dispatch_start"`
	DispatchEnd       time.Time `json:"dispatch_end"`
	Erected           bool      `json:"erected"`
	ReceiveInErection bool      `json:"receive_in_erection"`
}

type Stockyard struct {
	ID         int       `json:"id" example:"1"`
	YardName   string    `json:"yard_name" example:"Yard A"`
	Location   string    `json:"location" example:"Site North"`
	CreatedAt  time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt  time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	CarpetArea float64   `json:"carpet_area" example:"500.5"`
}

type VehicleDetails struct {
	ID              int       `json:"id" example:"1"`
	VehicleNumber   string    `json:"vehicle_number" example:"MH-01-AB-1234"`
	Status          string    `json:"status" example:"available"`
	CreatedAt       time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt       time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
	DriverName      string    `json:"driver_name" example:"Raj Kumar"`
	TruckType       string    `json:"truck_type" example:"flatbed"`
	DriverContactNo string    `json:"driver_contact_no" example:"9876543210"`
	TransporterID   int       `json:"transporter_id" example:"1"`
	Capacity        int       `json:"capacity" example:"20"`
}

type ReportIncidence struct {
	ID              int       `json:"id"`
	DispatchID      int       `json:"dispatch_id" `
	Type            string    `json:"type" `
	Comments        string    `json:"comments" `
	Photo           string    `json:"photo"`
	Status          string    `json:"status" `
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" `
	ReportingMember int       `json:"reporting_member" `
	ProjectID       int       `json:"project_id" `
}

type DispatchTrucks struct {
	VehicleID int `json:"vehicle_id" `
	TruckType int `json:"truck_type"`
}
type DispatchOrder struct {
	ID            int       `json:"id"`
	OrderNumber   string    `json:"dispatch_order_id"`
	ProjectID     int       `json:"project_id"`
	DispatchDate  time.Time `json:"dispatch_date"`
	UpdatedAt     time.Time `json:"updated_at"`
	VehicleId     int       `json:"vehicle_id"`
	DriverName    string    `json:"driver_name"`
	CurrentStatus string    `json:"current_staus"`
	ElementIDs    []int     `json:"element_id"`
}

// DispatchOrderItem represents individual items in a dispatch order
type DispatchOrderItem struct {
	ID              int       `json:"id"`
	DispatchOrderID int       `json:"dispatch_order_id"`
	ElementID       int       `json:"element_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// DispatchInlineItem tracks stock allocation for dispatch
type DispatchInlineItem struct {
	ID                  int       `json:"id"`
	DispatchOrderItemID int       `json:"dispatch_order_item_id"`
	StockID             int       `json:"stock_id"` // Links to stock table (precast_stock)
	AllocatedQuantity   int       `json:"allocated_quantity"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}
type DispatchIncidence struct {
	ID               int       `json:"id"`
	DispatchOrderID  int       `json:"dispatch_order_id"`
	IssueDescription string    `json:"issue_description"`
	ResolutionStatus string    `json:"resolution_status"` // e.g., "Pending", "Resolved"
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
type UpdateReceivedRequest struct {
	ElementIDs []int `json:"element_ids" binding:"required"`
	ProjectID  int   `json:"project_id" binding:"required"`
}

type StockErected struct {
	ID             int `json:"id"`
	PrecastStockID int `json:"precast_stock_id"`
	ProjectID      int `json:"project_id"`
}
type UpdateStockRequest struct {
	ElementID      int    `json:"element_id"`
	ApprovedStatus bool   `json:"approved_status"`
	Comments       string `json:"comments" binding:"required_if=ApprovedStatus false"`
}

// ElementTypeInterface represents the response structure for element type details
type ElementTypeInterface struct {
	ID                 int            `json:"id"`
	ElementType        string         `json:"element_type"`
	ElementTypeVersion string         `json:"element_type_version"`
	Thickness          float64        `json:"thickness"`
	Length             float64        `json:"length"`
	Height             float64        `json:"height"`
	Volume             float64        `json:"volume"`
	Mass               float64        `json:"mass"`
	Area               float64        `json:"area"`
	Width              float64        `json:"width"`
	TotalQuantity      int            `json:"total_quantity"`
	DrawingType        []DrawingType  `json:"DrawingType"`
	BOM                []BOMItem      `json:"BOM"`
	Stages             []StageDetails `json:"stages"`
}

type DrawingType struct {
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	FilePath  string            `json:"file_path"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Revision  []DrawingRevision `json:"revesion"`
}

type DrawingRevision struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	FilePath string `json:"file_path"`
}

type BOMItem struct {
	MaterialID int     `json:"material_id"`
	Name       string  `json:"name"`
	Quantity   float64 `json:"Quantity"`
	Unit       string  `json:"unit"`
	Rate       float64 `json:"rate"`
}

type StageDetails struct {
	StageID    int    `json:"stage_id"`
	StageName  string `json:"stage_name"`
	Quantity   int    `json:"quantity"`
	QC         int    `json:"qc"`
	Production int    `json:"production"`
}

// UpdateErectedStatusRequest represents the request structure for updating erected status
type UpdateErectedStatusRequest struct {
	ElementIDs []int `json:"element_ids" binding:"required"`
	ProjectID  int   `json:"project_id" binding:"required"`
}

// StockApprovalLog represents the structure for stock approval and rejection logs
type StockApprovalLog struct {
	ID              int       `json:"id"`
	PrecatStockID   int       `json:"precat_stock_id"`
	ElementID       int       `json:"element_id"`
	Status          string    `json:"status"`        // "Approved" or "Rejected"
	ActedBy         int       `json:"acted_by"`      // User ID
	ActedByName     string    `json:"acted_by_name"` // Full name of the user
	Comments        string    `json:"comments"`
	ActionTimestamp time.Time `json:"action_timestamp"`
	ElementType     string    `json:"element_type"`
	ElementTypeName string    `json:"element_type_name"`
}

// type QCAnswerResponse struct {
// 	Question string   `json:"question"`
// 	Answers  []string `json:"answers"`
// 	Image    *string  `json:"image,omitempty"` // Use pointer to omit null
// }

// UpdateStockErectedRequest represents the request structure for updating stock erected and precast stock
type UpdateStockErectedRequest struct {
	ElementIDs []int  `json:"element_ids" binding:"required"`
	ProjectID  int    `json:"project_id" binding:"required"`
	Comments   string `json:"comments"`
}

// PendingApprovalRequest represents a precast stock item that is pending approval
type PendingApprovalRequest struct {
	ID                  int       `json:"id"`
	ElementID           int       `json:"element_id"`
	ElementType         string    `json:"element_type"`
	ElementTypeID       int       `json:"element_type_id"`
	Dimensions          string    `json:"dimensions"`
	Weight              float64   `json:"weight"`
	ProductionDate      time.Time `json:"production_date"`
	StorageLocation     string    `json:"storage_location"`
	CreatedAt           time.Time `json:"created_at"`
	ElementTypeName     string    `json:"element_type_name"`
	ElementName         string    `json:"element_name"`
	ApprovalStatus      string    `json:"approval_status"`
	Comments            string    `json:"comments"`
	LastActionTimestamp time.Time `json:"last_action_timestamp"`
	ActedByName         string    `json:"acted_by_name"`
}

type DeletedElementWithDrawings struct {
	ID                 int               `json:"id"`
	ElementTypeID      int               `json:"element_type_id"`
	ElementID          string            `json:"element_id"`
	ElementName        string            `json:"element_name"`
	ProjectID          int               `json:"project_id"`
	CreatedBy          string            `json:"created_by"`
	CreatedAt          time.Time         `json:"created_at"`
	Status             string            `json:"status"`
	ElementTypeVersion string            `json:"element_type_version"`
	UpdateAt           time.Time         `json:"update_at"`
	TargetLocation     string            `json:"target_location"`
	DeletedBy          int               `json:"deleted_by"`
	DeletedByName      string            `json:"deleted_by_name"`
	Drawings           []DrawingResponse `json:"drawings"`
}

// ImportJob represents a background import job
type ImportJob struct {
	ID             int        `json:"id"`
	ProjectID      int        `json:"project_id"`
	JobType        string     `json:"job_type"`
	Status         string     `json:"status"`
	Progress       int        `json:"progress"`
	TotalItems     int        `json:"total_items"`
	ProcessedItems int        `json:"processed_items"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	Error          *string    `json:"error,omitempty"`
	Result         *string    `json:"result,omitempty"`
	FilePath       *string    `json:"file_path,omitempty"`
}

type EndClient struct {
	ID               int       `json:"id"`
	Email            string    `json:"email"`
	ContactPerson    string    `json:"contact_person"`
	Address          string    `json:"address"`
	Attachment       []string  `json:"attachment"`
	CIN              string    `json:"cin"`
	GSTNumber        string    `json:"gst_number"`
	PhoneNo          string    `json:"phone_no"`
	ProfilePicture   string    `json:"profile_picture"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	CreatedBy        int       `json:"created_by"`
	ClientID         int       `json:"client_id"`
	Organization     string    `json:"organization"`
	PhoneCode        int       `json:"phone_code"`
	PhoneCodeName    string    `json:"phone_code_name"`
	Abbreviation     string    `json:"abbreviation"`
	OrganizationName string    `json:"organization_name"`
}

type WorkOrder struct {
	ID                 int                 `json:"id"`
	WONumber           string              `json:"wo_number"`
	WODate             DateOnly            `json:"wo_date"`
	WOValidate         DateOnly            `json:"wo_validate"`
	TotalValue         float64             `json:"total_value"`
	ContactPerson      string              `json:"contact_person"`
	ContactEmail       string              `json:"contact_email"`
	ContactNumber      string              `json:"contact_number"`
	PhoneCode          int                 `json:"phone_code"`
	PaymentTerm        map[string]float64  `json:"payment_term"`
	WODescription      string              `json:"wo_description"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
	CreatedBy          int                 `json:"created_by"`
	UpdatedBy          string              `json:"updated_by"`
	Material           []WorkOrderMaterial `json:"material"`
	Attachments        []string            `json:"wo_attachment"`
	EndClientID        int                 `json:"endclient_id"`
	ProjectID          int                 `json:"project_id"`
	ProjectName        string              `json:"project_name"`
	EndClient          string              `json:"end_client"`
	Comments           string              `json:"comments"`
	RevisionNo         int                 `json:"revision_no"`
	PhoneCodeName      string              `json:"phone_code_name"`
	ShippedAddress     string              `json:"shipped_address"`
	BilledAddress      string              `json:"billed_address"`
	CreatedByName      string              `json:"created_by_name"`
	RecurrencePatterns []RecurrencePattern `json:"recurrence_patterns"`
}

type RecurrencePattern struct {
	PatternType string `json:"pattern_type"`          // "date" or "week"
	DateValue   string `json:"date_value,omitempty"`  // 1–31 or "penultimate"
	WeekNumber  string `json:"week_number,omitempty"` // "first", "second", "third", "fourth", "fifth"
	DayOfWeek   string `json:"day_of_week,omitempty"` // "monday"–"sunday"
	Stage       string `json:"stage,omitempty"`       // e.g., "draft", "sent"
}

type PendingInvoice struct {
	ID                int             `json:"id"`
	WorkOrderID       int             `json:"work_order_id"`
	Message           string          `json:"message"`
	GeneratedAt       time.Time       `json:"generated_at"`
	Status            string          `json:"status"`
	RecurrencePattern json.RawMessage `json:"recurrence_pattern"`
}

// ---------------------- REVISION TABLE MODEL ----------------------

type WorkOrderRevision struct {
	ID             int                           `json:"id"`
	WorkOrderID    int                           `json:"work_order_id"`
	RevisionNo     int                           `json:"revision_no"`
	WONumber       string                        `json:"wo_number"`
	WODate         *time.Time                    `json:"wo_date"`
	WOValidate     *time.Time                    `json:"wo_validate"`
	TotalValue     float64                       `json:"total_value"`
	ContactPerson  string                        `json:"contact_person"`
	PaymentTerm    map[string]float64            `json:"payment_term"`
	WODescription  string                        `json:"wo_description"`
	EndClientID    int                           `json:"endclient_id"`
	ProjectID      int                           `json:"project_id"`
	ContactEmail   string                        `json:"contact_email"`
	ContactNumber  string                        `json:"contact_number"`
	PhoneCode      int                           `json:"phone_code"`
	ShippedAddress string                        `json:"shipped_address"`
	BilledAddress  string                        `json:"billed_address"`
	CreatedBy      int                           `json:"created_by"`
	UpdatedAt      *time.Time                    `json:"updated_at"`
	Material       []WorkOrderMaterialRevision   `json:"material,omitempty"`
	Attachments    []WorkOrderAttachmentRevision `json:"attachments,omitempty"`
	CreatedByName  string                        `json:"created_by_name,omitempty"`
	EndClient      string                        `json:"endclient_name,omitempty"`
	ProjectName    string                        `json:"project_name,omitempty"`
	PhoneCodeName  string                        `json:"phone_code_name,omitempty"`
}

// ---------------------- MATERIAL REVISION MODEL ----------------------

type WorkOrderMaterialRevision struct {
	ID                  int      `json:"id"`
	WorkOrderRevisionID int      `json:"work_order_revision_id"`
	WorkOrderID         int      `json:"work_order_id"`
	ItemName            string   `json:"item_name"`
	UnitRate            float64  `json:"unit_rate"`
	Volume              float64  `json:"volume"`
	Tax                 float64  `json:"tax"`
	HsnCode             string   `json:"hsn_code"`
	TowerID             *int     `json:"tower_id,omitempty"`
	FloorID             []int    `json:"floor_id,omitempty"`
	TowerName           string   `json:"tower_name,omitempty"`
	FloorName           []string `json:"floor_name,omitempty"`
}

// ---------------------- ATTACHMENT REVISION MODEL ----------------------

type WorkOrderAttachmentRevision struct {
	ID                  int    `json:"id"`
	WorkOrderRevisionID int    `json:"work_order_revision_id"`
	WorkOrderID         int    `json:"work_order_id"`
	FileURL             string `json:"file_url"`
}

type WorkOrderMaterial struct {
	ID         int      `json:"id"`
	ItemName   string   `json:"item_name"`
	UnitRate   float64  `json:"unit_rate"`
	Volume     float64  `json:"volume"`
	HsnCode    int      `json:"hsn_code"`
	VolumeUsed float64  `json:"volume_used"`
	Tax        float64  `json:"tax"`
	RevisionID int      `json:"revision_no"`
	Balance    float64  `json:"balance"`
	TowerID    *int     `json:"tower_id"`
	FloorID    []int    `json:"floor_id"`
	TowerName  string   `json:"tower_name"`
	FloorName  []string `json:"floor_name"`
}

type WorkOrderAttachment struct {
	ID      int    `json:"id"`
	FileURL string `json:"file_url"`
}

type BOMMaster struct {
	MasterID int    `json:"master_bom_id" example:"1"`
	BOMName  string `json:"bom_name" example:"Cement"`
	BOMType  string `json:"bom_type" example:"200MM"`
	Unit     string `json:"unit" example:"Cum"`
}

type Invoice struct {
	ID              int           `json:"id" example:"1"`
	Name            *string       `json:"name,omitempty"`
	WorkOrderID     int           `json:"work_order_id" example:"1"`
	CreatedBy       int           `json:"created_by" example:"1"`
	CreatedAt       time.Time     `json:"created_at" example:"2024-01-15T10:30:00Z"`
	RevisionNo      int           `json:"revision_no" example:"1"`
	Items           []InvoiceItem `json:"items"`
	BillingAddress  string        `json:"billing_address" example:"123 Billing St"`
	ShippingAddress string        `json:"shipping_address" example:"456 Ship Rd"`
	CreatedByName   string        `json:"created_by_name" example:"Admin"`
	PaymentStatus   string        `json:"payment_status" example:"pending"`
	TotalPaid       float64       `json:"total_paid" example:"0"`
	TotalAmount     float64       `json:"total_amount" example:"100000"`
	UpdatedBy       int           `json:"updated_by" example:"1"`
	UpdatedAt       time.Time     `json:"updated_at" example:"2024-01-15T10:30:00Z"`
}

type GetInvoice struct {
	ID              int                `json:"id"`
	Name            *string            `json:"name,omitempty"`
	WorkOrderID     int                `json:"work_order_id"`
	CreatedBy       int                `json:"created_by"`
	CreatedAt       time.Time          `json:"created_at"`
	RevisionNo      int                `json:"revision_no"`
	Items           []GetInvoiceItem   `json:"items"`
	BillingAddress  string             `json:"billing_address"`
	ShippingAddress string             `json:"shipping_address"`
	CreatedByName   string             `json:"created_by_name"`
	WONumber        string             `json:"wo_number"`
	WODate          DateOnly           `json:"wo_date"`
	WOValidate      DateOnly           `json:"wo_validate"`
	TotalValue      float64            `json:"total_value"`
	ContactPerson   string             `json:"contact_person"`
	ContactEmail    string             `json:"contact_email"`
	ContactNumber   string             `json:"contact_number"`
	PhoneCode       int                `json:"phone_code"`
	PaymentTerm     map[string]float64 `json:"payment_term"`
	WODescription   string             `json:"wo_description"`
	EndClientID     int                `json:"endclient_id"`
	ProjectID       int                `json:"project_id"`
	ProjectName     string             `json:"project_name"`
	EndClient       string             `json:"end_client"`
	Comments        string             `json:"comments"`
	PhoneCodeName   string             `json:"phone_code_name"`
	WORevision      int                `json:"revision"`
	PaymentStatus   string             `json:"payment_status"`
	TotalPaid       float64            `json:"total_paid"`
	TotalAmount     float64            `json:"total_amount"`
	UpdatedBy       int                `json:"updated_by"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type GetInvoiceItem struct {
	ID         int      `json:"id"`
	InvoiceID  int      `json:"invoice_id"`
	ItemID     int      `json:"item_id"`
	Volume     float64  `json:"volume"`
	HSNCode    int      `json:"hsn_code"`
	TowerID    *int     `json:"tower_id,omitempty"`
	FloorID    []int    `json:"floor_id,omitempty"`
	TowerName  string   `json:"tower_name,omitempty"`
	FloorName  []string `json:"floor_name,omitempty"`
	ItemName   string   `json:"item_name"`
	UnitRate   float64  `json:"unit_rate"`
	Tax        float64  `json:"tax"`
	VolumeUsed float64  `json:"volume_used"`
	Balance    float64  `json:"balance"`
}

type InvoiceItem struct {
	ID        int     `json:"id"`
	InvoiceID int     `json:"invoice_id"`
	ItemID    int     `json:"item_id"`
	Volume    float64 `json:"volume"`
	HSNCode   int     `json:"hsn_code"`
}
type PrecastNew struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Prefix      string `json:"prefix"`
}

type CreatePrecastInput struct {
	ProjectID int64     `json:"project_id"`
	ParentID  *int64    `json:"parent_id"`
	Records   []Precast `json:"records"`
}
type Precast struct {
	ID               int    `json:"id"`
	ProjectID        int    `json:"project_id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	ParentID         *int64 `json:"parent_id"` // Pointer to handle NULL values
	Prefix           string `json:"prefix"`
	Path             string `json:"path"`
	NamingConvention string `json:"naming_convention"`
	Others           bool   `json:"others"`
}

type ElementInvoiceHistory struct {
	ID          int       `json:"id,omitempty"`
	WorkOrderID int       `json:"work_order_id"`
	ElementID   int       `json:"element_id"`
	ElementType int       `json:"element_type_id"`
	Stage       string    `json:"stage"` // casted/dispatch/erection/handover
	Volume      float64   `json:"volume"`
	Amount      float64   `json:"amount"`
	InvoiceID   int       `json:"invoice_id"`
	GeneratedAt time.Time `json:"generated_at"`
}

type Transporter struct {
	ID                 int       `json:"id" example:"1"`
	Name               string    `json:"name" example:"ABC Transport"`
	Address            string    `json:"address" example:"123 Logistics Park"`
	PhoneNo            string    `json:"phone_no" example:"9876543210"`
	EmergencyContactNo string    `json:"emergency_contact_no" example:"9876543211"`
	GstNo              string    `json:"gst_no" example:"27AABCU9603R1ZM"`
	CreatedAt          time.Time `json:"created_at" example:"2024-01-15T10:30:00Z"`
	UpdatedAt          time.Time `json:"updated_at" example:"2024-01-15T10:30:00Z"`
}

type ProjectCapabilities struct {
	HRA        bool `json:"hra"`
	WorkOrder  bool `json:"work_order"`
	Invoice    bool `json:"invoice"`
	Calculator bool `json:"calculator"`
}
