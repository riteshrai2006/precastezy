# Swagger API Documentation Guide

This project uses **swag** (github.com/swaggo/swag) to generate OpenAPI docs from handler comments. The generated spec is served at `/swagger/doc.json` and the Swagger UI at `/swagger/index.html`.

## Regenerating docs

After adding or changing handler annotations, run:

```bash
$(go env GOPATH)/bin/swag init -g main.go --parseDependency --parseInternal
```

This updates `docs/docs.go`, `docs/swagger.json`, and `docs/swagger.yaml`.

## Annotation pattern (per handler)

Place a **comment block** immediately above the handler function. Use the **exact path and method** from `main.go` for `@Router`.

### Minimal example

```go
// MyHandler does something.
// @Summary Short title
// @Description Optional longer description.
// @Tags TagName
// @Accept json
// @Produce json
// @Param id path int true "Resource ID"
// @Success 200 {object} models.MyResponse
// @Failure 400 {object} models.ErrorResponse
// @Failure 401 {object} models.ErrorResponse
// @Router /api/my_resource/{id} [get]
func MyHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) { ... }
}
```

### Request body

```go
// @Param body body models.MyRequest true "Request description"
```

For **array** body:

```go
// @Param body body []models.BOMProduct true "Array of items"
```

### Path and query params

```go
// @Param id path int true "ID"
// @Param project_id path int true "Project ID"
// @Param page query int false "Page"
// @Param search query string false "Search term"
// @Param stage query []string false "Stage filters"
```

### Responses

- **Single object:** `@Success 200 {object} models.SomeType`
- **Array:** `@Success 200 {array} models.SomeType`
- **Message only:** `@Success 200 {object} models.MessageResponse` or `models.SuccessResponse`
- **Errors:** `@Failure 400 {object} models.ErrorResponse` (and 401, 404, 500 as needed)

### Shared models (in `models` package)

- **ErrorResponse** – `error`, `details` (for all error responses)
- **MessageResponse** – `message` (for simple success like "Product deleted")
- **SuccessResponse** – `message`, `data` (generic success with optional data)
- **PaginatedResponse** – `data`, `pagination` (for list endpoints with pagination)
- **CreateClientRequest / CreateClientResponse** – client create
- **CreateBOMProductResponse**, **UpdateBOMProductResponse** – BOM APIs
- **LoginRequest**, **LoginResponse**, **SessionResponse**, **ValidateSessionResponse** – auth
- **UserResponse**, **CreateUserRequest**, **UpdateUserRequest** – user APIs

Define **new request/response types** in `models/ResponseModel.go` (or appropriate models file) when an API has a specific JSON shape, so the docs show real request/response schemas.

## Handlers already documented (inputs/outputs)

- **BOM:** CreateBOMProduct, GetAllBOMProducts, GetBOMProductByID, GetAllBOMProductsProjectId, UpdateBOMProduct, DeleteBOMProduct
- **Clients:** SearchClients, GetAllClient, GetClientByID, CreateClient, UpdateClient
- **Warehouses:** CreateWarehouse, GetWarehouses, GetWarehouseById, GetWarehousesProjectId, UpdateWarehouse, DeleteWarehouse (all with request/response)
- **Vendors:** CreateVendor, GetVendors, GetVendorByID, GetVendorsProjectId, UpdateVendor, DeleteVendor (all with request/response)
- **Projects:** CreateProject, UpdateeProject, DeleteProject, FetchProject, FetchAllProjects
- **QC:** GetAllQCStatuses, GetQCStatus, CreateQCStatus, UpdateQCStatus, DeleteQCStatus
- **Stockyards:** GetStockyard, CreateStockyard, GetStockyardByID, UpdateStockyard, DeleteStockyard
- **Vehicles:** CreateVehicleDetails, GetAllVehicles, GetVehicleByID, UpdateVehicleDetails, DeleteVehicleDetails
- **Project Members:** CreateMember, GetMembers, UpdateMember, DeleteMember, ExportMembersPDF
- **Drawings:** DeleteDrawing, GetAllDrawings, GetDrawingByDrawingID, GetDrawingsByProjectID, GetDrawingsByElementType
- **Drawing Update:** UpdateDrawingHandler
- **Drawing Revisions:** GetDrawingRevisionByProjectID, GetDrawingRevisionByRevisionId
- **Notifications:** GetMyNotificationsHandler, MarkNotificationAsReadHandler, MarkAllNotificationsAsReadHandler, RegisterFCMTokenHandler, RemoveFCMTokenHandler
- **Elements:** Element (create), DeleteElement, GetElementsWithDrawingsByProjectId, GetElementsWithDrawingsByElementId, GetAllElementsWithDrawings, GetElementsByElementTypeID, GetAllElements
- **Drawing types:** CreateDrawingType, GetAllDrawingType, GetAllDrawingTypeByprojectid, UpdateDrawingType, GetDrawingTypeByID (GetDrawingTypeByID route: `/drawing-type/{id}` without `/api` per main.go)
- **Upload:** UploadFile (POST /api/upload), ServeFile (GET /api/get-file)
- **Settings:** CreateSettingHandler (POST /api/settings), GetSettingHandler (GET /api/settings/{user_id}), GetUserSessionInfo (GET /api/settings/user/{user_id}/sessions)
- **Dispatch:** CreateAndSaveDispatchOrder, GetDispatchOrdersByProjectID, GenerateDispatchPDF, ReceiveDispatchOrderByErection, UpdateDispatchToInTransit, GetDispatchTrackingLogs
- **Auth:** Login, ValidateSession, Session, etc. (see LoginHandlers, ValidateSession)
- **Users:** CreateUser, UpdateUser, GetUser, GetAllUsers (see UserHandlers)
- **Inventory:** CreatePurchase, FetchAllInvPurchases, FetchInvPurchaseByID, FetchAllInvLineItems, FetchInvLineItemByID, FetchAllInvTransactions, FetchInvTransactionByID, FetchAllInvTracks, FetchInvTrackByID, InventoryView, InventoryViewProjectId, InventoryViewEachBOM, CheckInventoryShortage, GeneratePurchaseRequest, GetInventoryShortageSummary
- **Invoices:** CreateInvoice (and other invoice endpoints in InvoiceHandler – add as needed)
- **End clients:** CreateEndClient, GetEndClients, GetEndClient, UpdateEndClient, DeleteEndClient, GetEndClientsByClient
- **Units:** CreateUnit, GetUnits, GetUnitByID, UpdateUnit, DeleteUnit
- **Currency:** CreateCurrency, GetCurrencies, GetCurrencyByID, UpdateCurrency, DeleteCurrency
- **Phone codes:** CreatePhoneCode, GetAllPhoneCodes, GetPhoneCode, UpdatePhoneCode, DeletePhoneCode
- **Transporters:** CreateTransporter, GetAllTransporters, GetTransporterByID, UpdateTransporter, DeleteTransporter
- **QR:** GenerateQRCodeJPEG
- **Auth (password):** ForgetPasswordHandler, ResetPasswordHandler
- **Activity logs:** GetActivityLogsHandler, SearchActivityLogsHandler
- **Element type BOM:** GetAllBOMPros, GetBOMPro, GetBOMProByProjectId, GetBOMProByElementTypeID (`/api/get_bom`, `/api/get_bom/{id}`, `/api/bom_get_fetch/{project_id}`, `/api/bom_fetch/{element_type_id}`)
- **Projects (extra):** GetProject (`project_get/{project_id}`), GetProjectsByRole (`project_by_role`), GetProjectRoles (`project_roles/{project_id}`), UpdateProject (`project_update/{project_id}`)
- **Work orders:** CreateWorkOrder, GetWorkOrder, GetAllWorkOrders, UpdateWorkOrder, DeleteWorkOrder, GetWorkOrderRevisions, CreateWorkOrderAmendment, SearchWorkOrders
- **Erection:** RageStockRequestByErection, UpdateErectedStatus, UpdateStockErectedWhenErected, UpdateStockByPlaning
- **Precast stock:** InPrecastStock, ReceivedPrecastStock, UpdateStockyardReceived
- **Rectification:** UpdateRectificationHandler
- **Export:** ExportCSVBOM, ExportCSVPrecast, ExportExcellementType; ExportAllViewsPDF (Kanban), ExportDashboardPDF (DashboardHandler), GetElementReportAccordingToStage (PrecastReportHandlers)
- **Element types (extra):** FetchElementTypeByID, FetchElementTypesName, CreateElementTypeName, GetAllelementType, SearchElementTypes, GetElementDetailsByTypeAndLocation
- **Categories, Departments, Skills, People, ManpowerCount, EmailTemplate, ElementType, Precast, Jobs (GormJobHandlers), PDF summary** – various endpoints already have annotations; **@Router** paths use `/api/` prefix to match `main.go`

## Adding docs for a new API

1. **In the handler file:** Add the comment block above the handler with `@Summary`, `@Description`, `@Tags`, `@Param`, `@Success`, `@Failure`, and **@Router** matching the route in `main.go` (path + method).
2. **If the API uses a body or returns a custom shape:** Define a struct in `models` (e.g. `models.MyRequest`, `models.MyResponse`) with `json` tags and reference it in the annotations.
3. Run `swag init -g main.go --parseDependency --parseInternal`.
4. Fix any “cannot find type” errors by adding the missing model in `models`.

## Verification

- `swag init` must exit with code 0.
- `go build ./...` must succeed.
- Open `/swagger/index.html` and confirm the endpoint shows the correct request/response schemas.
