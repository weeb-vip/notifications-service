package db

import (
	"context"
	"fmt"

	"github.com/weeb-vip/notifications-service/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

const (
	callbackBeforeCreate = "tracing:before_create"
	callbackAfterCreate  = "tracing:after_create"
	callbackBeforeQuery  = "tracing:before_query"
	callbackAfterQuery   = "tracing:after_query"
	callbackBeforeUpdate = "tracing:before_update"
	callbackAfterUpdate  = "tracing:after_update"
	callbackBeforeDelete = "tracing:before_delete"
	callbackAfterDelete  = "tracing:after_delete"

	spanKey = "gorm:span"
)

type TracingPlugin struct{}

func (tp *TracingPlugin) Name() string {
	return "TracingPlugin"
}

func (tp *TracingPlugin) Initialize(db *gorm.DB) error {
	// Register callbacks for Create operations
	db.Callback().Create().Before("gorm:create").Register(callbackBeforeCreate, tp.beforeCreate)
	db.Callback().Create().After("gorm:create").Register(callbackAfterCreate, tp.afterCreate)

	// Register callbacks for Query operations
	db.Callback().Query().Before("gorm:query").Register(callbackBeforeQuery, tp.beforeQuery)
	db.Callback().Query().After("gorm:query").Register(callbackAfterQuery, tp.afterQuery)

	// Register callbacks for Update operations
	db.Callback().Update().Before("gorm:update").Register(callbackBeforeUpdate, tp.beforeUpdate)
	db.Callback().Update().After("gorm:update").Register(callbackAfterUpdate, tp.afterUpdate)

	// Register callbacks for Delete operations
	db.Callback().Delete().Before("gorm:delete").Register(callbackBeforeDelete, tp.beforeDelete)
	db.Callback().Delete().After("gorm:delete").Register(callbackAfterDelete, tp.afterDelete)

	return nil
}

func (tp *TracingPlugin) beforeCreate(db *gorm.DB) {
	tp.before(db, "CREATE")
}

func (tp *TracingPlugin) afterCreate(db *gorm.DB) {
	tp.after(db)
}

func (tp *TracingPlugin) beforeQuery(db *gorm.DB) {
	tp.before(db, "SELECT")
}

func (tp *TracingPlugin) afterQuery(db *gorm.DB) {
	tp.after(db)
}

func (tp *TracingPlugin) beforeUpdate(db *gorm.DB) {
	tp.before(db, "UPDATE")
}

func (tp *TracingPlugin) afterUpdate(db *gorm.DB) {
	tp.after(db)
}

func (tp *TracingPlugin) beforeDelete(db *gorm.DB) {
	tp.before(db, "DELETE")
}

func (tp *TracingPlugin) afterDelete(db *gorm.DB) {
	tp.after(db)
}

func (tp *TracingPlugin) before(db *gorm.DB, operation string) {
	ctx := db.Statement.Context
	if ctx == nil {
		ctx = context.Background()
	}

	// Try to get tracer from context first, then fall back to global tracer
	tracer := tracing.GetTracer(ctx)
	if tracer == nil {
		// Use global tracer as fallback
		tracer = otel.Tracer("gorm")
	}

	spanName := fmt.Sprintf("GORM %s", operation)

	// Use the existing context to create a child span
	newCtx, span := tracer.Start(ctx, spanName,
		trace.WithAttributes(
			attribute.String("db.system", "mysql"),
			attribute.String("db.operation", operation),
			attribute.String("db.table", db.Statement.Table),
		),
		trace.WithSpanKind(trace.SpanKindClient),
		tracing.GetEnvironmentAttribute(),
	)

	// Update the statement context to include the new span
	db.Statement.Context = newCtx

	// Store span in DB instance for later use
	db.Set(spanKey, span)
}

func (tp *TracingPlugin) after(db *gorm.DB) {
	// Retrieve span from DB instance
	value, ok := db.Get(spanKey)
	if !ok {
		return
	}

	span, ok := value.(trace.Span)
	if !ok {
		return
	}

	// Add SQL query and additional database attributes
	if db.Statement != nil && db.Statement.SQL.String() != "" {
		span.SetAttributes(
			attribute.String("db.statement", db.Statement.SQL.String()),
			attribute.String("db.name", "weeb"),
			attribute.String("db.user", "weeb"),
		)
	}

	// Add rows affected and performance metrics
	if db.Statement != nil {
		span.SetAttributes(
			attribute.Int64("db.rows_affected", db.RowsAffected),
			attribute.String("db.operation.name", db.Statement.Table+"."+db.Statement.SQL.String()[:20]),
		)
	}

	// Handle errors
	if db.Error != nil && db.Error != gorm.ErrRecordNotFound {
		span.RecordError(db.Error)
		span.SetStatus(codes.Error, db.Error.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	span.End()
}
