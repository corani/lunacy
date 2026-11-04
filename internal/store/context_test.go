package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestStoreOperationsHonorCancellation(t *testing.T) {
	st, _ := testStore(t)
	must(t, st.UpsertBook(t.Context(), Book{Filename: "a", Title: "A"}))
	must(t, st.UpsertReadState(t.Context(), "a", 1))
	must(t, st.UpsertAnnotation(t.Context(), Annotation{Filename: "a", AnnID: 1}))
	before := getSync(t, st, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := st.GetBook(ctx, "a")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("read ignored cancellation: %v", err)
	}
	for name, operation := range map[string]func() error{
		"update":             func() error { return st.UpsertReadState(ctx, "a", 3) },
		"delete all books":   func() error { return st.DeleteBooksNotIn(ctx, nil) },
		"delete annotations": func() error { return st.DeleteAnnotationsByFile(ctx, "a") },
	} {
		if err := operation(); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s ignored cancellation: %v", name, err)
		}
	}
	after := getSync(t, st, nil)
	if !reflect.DeepEqual(before.Books, after.Books) || !reflect.DeepEqual(before.Annotations, after.Annotations) {
		t.Fatal("canceled operations changed data or timestamps")
	}
}

func TestCanceledMutationRollsBackAndReleasesConnection(t *testing.T) {
	st, _ := testStore(t)
	must(t, st.UpsertBook(t.Context(), Book{Filename: "a"}))
	must(t, st.UpsertReadState(t.Context(), "a", 1))
	st.db.SetMaxOpenConns(1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err := st.MutateBook(ctx, "a", func(m *Mutation) error {
		if err := m.ReadState(3); err != nil {
			return err
		}
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("mutation ignored cancellation: %v", err)
	}
	book, err := st.GetBook(t.Context(), "a")
	must(t, err)
	if book.ReadState != 1 {
		t.Fatal("canceled transaction was not rolled back")
	}
	// Reuse the only connection: cleanup must not leave an active transaction.
	must(t, st.UpsertReadState(t.Context(), "a", 3))
}
