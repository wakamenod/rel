package grimoire

import (
	"reflect"
	"strings"
	"time"

	"github.com/Fs02/go-paranoid"
	"github.com/Fs02/grimoire/c"
	"github.com/Fs02/grimoire/changeset"
	"github.com/Fs02/grimoire/errors"
	"github.com/Fs02/grimoire/internal"
	"github.com/azer/snakecase"
)

// Query defines information about query generated by query builder.
type Query struct {
	repo            *Repo
	Collection      string
	Fields          []string
	AsDistinct      bool
	JoinClause      []c.Join
	Condition       c.Condition
	GroupFields     []string
	HavingCondition c.Condition
	OrderClause     []c.Order
	OffsetResult    int
	LimitResult     int
	Changes         map[string]interface{}
}

// Select filter fields to be selected from database.
func (query Query) Select(fields ...string) Query {
	query.Fields = fields
	return query
}

// Distinct add distinct option to select query.
func (query Query) Distinct() Query {
	query.AsDistinct = true
	return query
}

// Join current collection with other collection.
func (query Query) Join(collection string, condition ...c.Condition) Query {
	return query.JoinWith("JOIN", collection, condition...)
}

// JoinWith current collection with other collection with custom join mode.
func (query Query) JoinWith(mode string, collection string, condition ...c.Condition) Query {
	if len(condition) == 0 {
		query.JoinClause = append(query.JoinClause, c.Join{
			Mode:       mode,
			Collection: collection,
			Condition: c.And(c.Eq(
				c.I(query.Collection+"."+strings.TrimSuffix(collection, "s")+"_id"),
				c.I(collection+".id"),
			)),
		})
	} else {
		query.JoinClause = append(query.JoinClause, c.Join{
			Mode:       mode,
			Collection: collection,
			Condition:  c.And(condition...),
		})
	}

	return query
}

// Where expressions are used to filter the result set. If there is more than one where expression, they are combined with an and operator.
func (query Query) Where(condition ...c.Condition) Query {
	query.Condition = query.Condition.And(condition...)
	return query
}

// OrWhere behaves exactly the same as where except it combines with any previous expression by using an OR.
func (query Query) OrWhere(condition ...c.Condition) Query {
	query.Condition = query.Condition.Or(c.And(condition...))
	return query
}

// Group query using fields.
func (query Query) Group(fields ...string) Query {
	query.GroupFields = fields
	return query
}

// Having adds condition for group query.
func (query Query) Having(condition ...c.Condition) Query {
	query.HavingCondition = query.HavingCondition.And(condition...)
	return query
}

// OrHaving behaves exactly the same as having except it combines with any previous expression by using an OR.
func (query Query) OrHaving(condition ...c.Condition) Query {
	query.HavingCondition = query.HavingCondition.Or(c.And(condition...))
	return query
}

// Order the result returned by database.
func (query Query) Order(order ...c.Order) Query {
	query.OrderClause = append(query.OrderClause, order...)
	return query
}

// Offset the result returned by database.
func (query Query) Offset(offset int) Query {
	query.OffsetResult = offset
	return query
}

// Limit result returned by database.
func (query Query) Limit(limit int) Query {
	query.LimitResult = limit
	return query
}

// Find adds where id=? into query.
// This is short cut for Where(Eq(I("id"), 1))
func (query Query) Find(id interface{}) Query {
	return query.Where(c.Eq(c.I(query.Collection+".id"), id))
}

// Set value for insert or update operation that will replace changeset value.
func (query Query) Set(field string, value interface{}) Query {
	if query.Changes == nil {
		query.Changes = make(map[string]interface{})
	}

	query.Changes[field] = value
	return query
}

// One retrieves one result that match the query.
// If no result found, it'll return not found error.
func (query Query) One(record interface{}) error {
	query.LimitResult = 1
	count, err := query.repo.adapter.All(query, record, query.repo.logger...)

	if err != nil {
		return errors.Wrap(err)
	} else if count == 0 {
		return errors.NotFoundError("no result found")
	} else {
		return nil
	}
}

// MustOne retrieves one result that match the query.
// If no result found, it'll panic.
func (query Query) MustOne(record interface{}) {
	paranoid.Panic(query.One(record))
}

// All retrieves all results that match the query.
func (query Query) All(record interface{}) error {
	_, err := query.repo.adapter.All(query, record, query.repo.logger...)
	return err
}

// MustAll retrieves all results that match the query.
// It'll panic if any error eccured.
func (query Query) MustAll(record interface{}) {
	paranoid.Panic(query.All(record))
}

// Count retrieves count of results that match the query.
func (query Query) Count() (int, error) {
	count, err := query.repo.adapter.Count(query, query.repo.logger...)
	return count, err
}

// MustCount retrieves count of results that match the query.
// It'll panic if any error eccured.
func (query Query) MustCount() int {
	count, err := query.Count()
	paranoid.Panic(err)
	return count
}

// Insert records to database.
func (query Query) Insert(record interface{}, chs ...*changeset.Changeset) error {
	var err error
	var ids []interface{}

	if len(chs) == 1 {
		// single insert
		ch := chs[0]
		changes := make(map[string]interface{})
		cloneChangeset(changes, ch.Changes())
		putTimestamp(changes, "created_at", ch.Types())
		putTimestamp(changes, "updated_at", ch.Types())
		cloneQuery(changes, query.Changes)

		var id interface{}
		id, err = query.repo.adapter.Insert(query, changes, query.repo.logger...)
		ids = append(ids, id)
	} else if len(chs) > 1 {
		// multiple insert
		fields := getFields(query, chs)

		allchanges := make([]map[string]interface{}, len(chs))
		for i, ch := range chs {
			changes := make(map[string]interface{})
			cloneChangeset(changes, ch.Changes())
			putTimestamp(changes, "created_at", ch.Types())
			putTimestamp(changes, "updated_at", ch.Types())
			cloneQuery(changes, query.Changes)

			allchanges[i] = changes
		}

		ids, err = query.repo.adapter.InsertAll(query, fields, allchanges, query.repo.logger...)
	} else if len(query.Changes) > 0 {
		// set only
		var id interface{}
		id, err = query.repo.adapter.Insert(query, query.Changes, query.repo.logger...)
		ids = append(ids, id)
	}

	if err != nil {
		return errors.Wrap(err)
	} else if record == nil || len(ids) == 0 {
		return nil
	} else if len(ids) == 1 {
		return errors.Wrap(query.Find(ids[0]).One(record))
	}

	return errors.Wrap(query.Where(c.In(c.I("id"), ids...)).All(record))
}

// MustInsert records to database.
// It'll panic if any error occurred.
func (query Query) MustInsert(record interface{}, chs ...*changeset.Changeset) {
	paranoid.Panic(query.Insert(record, chs...))
}

// Update records in database.
// It'll panic if any error occurred.
func (query Query) Update(record interface{}, chs ...*changeset.Changeset) error {
	changes := make(map[string]interface{})

	// only take the first changeset if any
	if len(chs) != 0 {
		cloneChangeset(changes, chs[0].Changes())
		putTimestamp(changes, "updated_at", chs[0].Types())
	}

	cloneQuery(changes, query.Changes)

	// nothing to update
	if len(changes) == 0 {
		return nil
	}

	// perform update
	err := query.repo.adapter.Update(query, changes, query.repo.logger...)
	if err != nil {
		return errors.Wrap(err)
	}

	// should not fetch updated record(s) if not necessery
	if record != nil {
		return errors.Wrap(query.All(record))
	}

	return nil
}

// MustUpdate records in database.
// It'll panic if any error occurred.
func (query Query) MustUpdate(record interface{}, chs ...*changeset.Changeset) {
	paranoid.Panic(query.Update(record, chs...))
}

func cloneChangeset(out map[string]interface{}, changes map[string]interface{}) {
	for k, v := range changes {
		// skip if not scannable
		if !internal.Scannable(reflect.TypeOf(v)) {
			continue
		}

		out[k] = v
	}
}

func cloneQuery(out map[string]interface{}, changes map[string]interface{}) {
	for k, v := range changes {
		out[k] = v
	}
}

func putTimestamp(out map[string]interface{}, field string, types map[string]reflect.Type) {
	if typ, ok := types[field]; ok && typ == reflect.TypeOf(time.Time{}) {
		out[field] = time.Now().Round(time.Second)
	}
}

func getFields(query Query, chs []*changeset.Changeset) []string {
	fields := make([]string, 0, len(chs[0].Types()))

	for f := range chs[0].Types() {
		if f == "created_at" || f == "updated_at" {
			fields = append(fields, f)
			continue
		}

		if _, exist := query.Changes[f]; exist {
			fields = append(fields, f)
		}

		for _, ch := range chs {
			if _, exist := ch.Changes()[f]; exist {
				// skip if not scannable
				if !internal.Scannable(ch.Types()[f]) {
					break
				}

				fields = append(fields, f)
				break
			}
		}
	}

	return fields
}

// Save a record to database.
// If condition exist, put will try to update the record, otherwise it'll insert it.
// Save ignores id from record.
func (query Query) Save(record interface{}) error {
	rv := reflect.ValueOf(record)
	rt := rv.Type()
	if rt.Kind() == reflect.Ptr && rt.Elem().Kind() == reflect.Slice {
		// Put multiple records
		rv = rv.Elem()

		// if it's an empty slice, do nothing
		if rv.Len() == 0 {
			return nil
		}

		if query.Condition.None() {
			// InsertAll
			chs := []*changeset.Changeset{}

			for i := 0; i < rv.Len(); i++ {
				ch := changeset.Change(rv.Index(i).Interface())
				changeset.DeleteChange(ch, "id")
				chs = append(chs, ch)
			}

			return query.Insert(record, chs...)
		}

		// Update only with first record definition.
		ch := changeset.Change(rv.Index(0).Interface())
		changeset.DeleteChange(ch, "id")
		changeset.DeleteChange(ch, "created_at")
		return query.Update(record, ch)
	}

	// Put single records
	ch := changeset.Change(record)
	changeset.DeleteChange(ch, "id")

	if query.Condition.None() {
		return query.Insert(record, ch)
	}

	// remove created_at from changeset
	changeset.DeleteChange(ch, "created_at")

	return query.Update(record, ch)
}

// MustSave puts a record to database.
// It'll panic if any error eccured.
func (query Query) MustSave(record interface{}) {
	paranoid.Panic(query.Save(record))
}

// Delete deletes all results that match the query.
func (query Query) Delete() error {
	return errors.Wrap(query.repo.adapter.Delete(query, query.repo.logger...))
}

// MustDelete deletes all results that match the query.
// It'll panic if any error eccured.
func (query Query) MustDelete() {
	paranoid.Panic(query.Delete())
}

type preloadInfo struct {
	schema reflect.Value
	field  reflect.Value
}

func (query Query) Preload(record interface{}, field string) error {
	path := strings.Split(field, ".")

	rv := reflect.ValueOf(record)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		panic("grimoire: record parameter must be a pointer")
	}

	preload := traversePreloadTarget(rv.Elem(), path)
	if len(preload) == 0 {
		return errors.UnexpectedError("nothing to preload")
	}

	schemaType := preload[0].schema.Type()
	_, ref, fk := getAssocInfo(schemaType, path[len(path)-1])
	fieldType := preload[0].field.Type()

	if fieldType.Kind() == reflect.Slice || fieldType.Kind() == reflect.Array {
		fieldType = fieldType.Elem()
	}

	// get db field name.
	// TODO: handle db tag
	fieldName := snakecase.SnakeCase(fk)

	var refIndex []int
	if idv, exist := schemaType.FieldByName(ref); !exist {
		panic("grimoire: ref field not found " + ref)
	} else {
		refIndex = idv.Index
	}

	var fkIndex []int
	if idv, exist := fieldType.FieldByName(fk); !exist {
		panic("grimoire: fk field not found " + fk)
	} else {
		fkIndex = idv.Index
	}

	// collect ids.
	addrs := make(map[interface{}][]reflect.Value)
	ids := []interface{}{}

	for _, pre := range preload {
		id := pre.schema.FieldByIndex(refIndex).Interface()
		addrs[id] = append(addrs[id], pre.field)

		// add to ids if not yet added.
		if len(addrs[id]) == 1 {
			ids = append(ids, id)
		}
	}

	// prepare temp result variable for querying
	rt := preload[0].field.Type()
	if rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array || rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	slice := reflect.MakeSlice(reflect.SliceOf(rt), 0, len(ids))
	result := reflect.New(slice.Type())
	result.Elem().Set(slice)

	// query all records usinc collected ids.
	query.Where(c.In(c.I(fieldName), ids...)).All(result.Interface())

	// map results.
	result = result.Elem()
	for i := 0; i < result.Len(); i++ {
		curr := result.Index(i)
		key := curr.FieldByIndex(fkIndex).Interface()

		for _, addr := range addrs[key] {
			if addr.Kind() == reflect.Slice {
				addr.Set(reflect.Append(addr, curr))
			} else {
				addr.Set(curr)
			}
		}
	}

	return nil
}

func traversePreloadTarget(rv reflect.Value, path []string) []preloadInfo {
	result := []preloadInfo{}
	rt := rv.Type()

	if rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array {
		for i := 0; i < rv.Len(); i++ {
			result = append(result, traversePreloadTarget(rv.Index(i), path)...)
		}
		return result
	}

	if rt.Kind() == reflect.Ptr {
		rv = rv.Elem()
		rt = rv.Type()
	}

	if rt.Kind() != reflect.Struct {
		panic("grimoire: preload field must be a struct")
	}

	// forward to next path.
	fv := rv.FieldByName(path[0])
	if !fv.IsValid() {
		panic("grimoire: field not found " + path[0])
	}

	if fv.Kind() == reflect.Ptr {
		fv = fv.Elem()
	}

	if len(path) == 1 {
		result = append(result, preloadInfo{
			schema: rv,
			field:  fv,
		})
	} else {
		result = append(result, traversePreloadTarget(fv, path[1:])...)
	}

	return result
}

func getAssocInfo(rt reflect.Type, field string) (string, string, string) {
	ft, _ := rt.FieldByName(field)
	assoc := ft.Tag.Get("assoc")
	ref := ft.Tag.Get("ref")
	fk := ft.Tag.Get("fk")

	return assoc, ref, fk
}
