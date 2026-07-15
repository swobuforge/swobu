// Package target_edit owns the inline route target mutation workflow.
//
// Target create, edit, and delete stay in the expanded route plane rather than
// opening a wizard or provider-management surface. The workflow owns draft
// target fields and validation; route sections only decide which route/target
// currently hosts the workflow.
package target_edit
