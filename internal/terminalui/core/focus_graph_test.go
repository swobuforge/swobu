package core

import "testing"

func TestCompileFocusGraph_EmptyTree(t *testing.T) {
	g := CompileFocusGraph(Box[struct{}]())
	if !g.Empty() {
		t.Fatal("empty tree yields empty graph")
	}
	if len(g.Order) != 0 {
		t.Fatalf("order len = %d, want 0", len(g.Order))
	}
	if len(g.Roots) != 0 {
		t.Fatalf("roots len = %d, want 0", len(g.Roots))
	}
}

func TestCompileFocusGraph_SingleFocusable(t *testing.T) {
	root := Input[struct{}]("hello")
	g := CompileFocusGraph(root)
	if g.Empty() {
		t.Fatal("non-empty")
	}
	if len(g.Order) != 1 {
		t.Fatalf("order len = %d, want 1", len(g.Order))
	}
	if len(g.Roots) != 1 {
		t.Fatalf("roots len = %d, want 1", len(g.Roots))
	}
	node := g.ByID[g.Order[0]]
	if node.Mode != Focusable {
		t.Fatalf("mode = %v, want Focusable", node.Mode)
	}
	if !node.ParentID.Empty() {
		t.Fatal("expected no parent")
	}
}

func TestCompileFocusGraph_PreservesExplicitID(t *testing.T) {
	root := Text[struct{}]("a").Interaction(InteractionSpec[struct{}]{
		Focus: FocusSpec{Mode: Focusable, ID: FocusID("my-id")},
	})
	g := CompileFocusGraph(root)
	if len(g.Order) != 1 {
		t.Fatalf("count = %d, want 1", len(g.Order))
	}
	if g.Order[0] != FocusID("my-id") {
		t.Fatalf("id = %v, want my-id", g.Order[0])
	}
}

func TestCompileFocusGraph_UsesNodeKeyWhenNoID(t *testing.T) {
	root := Text[struct{}]("a").Key(K("keyed")).Interaction(InteractionSpec[struct{}]{
		Focus: FocusSpec{Mode: Focusable},
	})
	g := CompileFocusGraph(root)
	if g.Order[0] != FocusID("keyed") {
		t.Fatalf("id = %v, want keyed", g.Order[0])
	}
}

func TestCompileFocusGraph_SkipsNonFocusable(t *testing.T) {
	root := Box[struct{}](
		Text[struct{}]("a"),
		Input[struct{}]("b"),
	)
	g := CompileFocusGraph(root)
	if len(g.Order) != 1 {
		t.Fatalf("count = %d, want 1", len(g.Order))
	}
	if len(g.ByID) != 1 {
		t.Fatalf("map size = %d, want 1", len(g.ByID))
	}
}

func TestCompileFocusGraph_ScopeCreatesHierarchy(t *testing.T) {
	root := Box[struct{}](
		Box[struct{}](
			Input[struct{}]("child1"),
		).Key(K("sc")).Interaction(InteractionSpec[struct{}]{
			Focus: FocusSpec{Mode: FocusScope, ID: FocusID("scope1")},
		}),
	)
	g := CompileFocusGraph(root)
	if len(g.Order) != 2 {
		t.Fatalf("count = %d, want 2", len(g.Order))
	}
	scopeNode := g.ByID[FocusID("scope1")]
	if len(scopeNode.Children) != 1 {
		t.Fatalf("scope children = %d, want 1", len(scopeNode.Children))
	}
	childNode := g.ByID[scopeNode.Children[0]]
	if childNode.ParentID != FocusID("scope1") {
		t.Fatalf("child parent = %v, want scope1", childNode.ParentID)
	}
}

func TestCompileFocusGraph_GroupChildren(t *testing.T) {
	root := Box[struct{}](
		Box[struct{}](
			Text[struct{}]("a").Key(K("a")).Interaction(InteractionSpec[struct{}]{
				Focus: FocusSpec{Mode: Focusable, ID: FocusID("a")},
			}),
			Text[struct{}]("b").Key(K("b")).Interaction(InteractionSpec[struct{}]{
				Focus: FocusSpec{Mode: Focusable, ID: FocusID("b")},
			}),
		).Key(K("gp")).Interaction(InteractionSpec[struct{}]{
			Focus: FocusSpec{Mode: FocusGroup, ID: FocusID("group1")},
		}),
	)
	g := CompileFocusGraph(root)
	if len(g.Order) != 3 {
		t.Fatalf("count = %d, want 3", len(g.Order))
	}
	groupNode := g.ByID[FocusID("group1")]
	if len(groupNode.Children) != 2 {
		t.Fatalf("group children = %d, want 2", len(groupNode.Children))
	}
}

func TestCompileFocusGraph_OrderMatchesPreOrder(t *testing.T) {
	root := Box[struct{}](
		Text[struct{}]("a").Key(K("a")).Interaction(InteractionSpec[struct{}]{
			Focus: FocusSpec{Mode: Focusable, ID: FocusID("a")},
		}),
		Text[struct{}]("b").Key(K("b")).Interaction(InteractionSpec[struct{}]{
			Focus: FocusSpec{Mode: Focusable, ID: FocusID("b")},
		}),
		Text[struct{}]("c").Key(K("c")).Interaction(InteractionSpec[struct{}]{
			Focus: FocusSpec{Mode: Focusable, ID: FocusID("c")},
		}),
	)
	g := CompileFocusGraph(root)
	want := []FocusID{"a", "b", "c"}
	if len(g.Order) != len(want) {
		t.Fatalf("order len = %d, want %d", len(g.Order), len(want))
	}
	for i := range want {
		if g.Order[i] != want[i] {
			t.Fatalf("order[%d] = %v, want %v", i, g.Order[i], want[i])
		}
	}
}
