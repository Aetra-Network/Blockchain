package app

import (
	"fmt"

	"cosmossdk.io/core/appmodule"
)

// ValidateAetraCoreWiringGate is called from app.go during construction and
// PANICS the binary on error. Its policy is documented in
// docs/security/phase9-aether-core-wiring-gate.md; the two must be amended
// together.
//
// AEZ Phase 2 amendment -- what changed, and what deliberately did not:
//
// x/aez moved from the prototype family to the system family. The gate checks
// BOTH families with the SAME three rules (registered, store key mounted, no
// module-account permissions unless the name is a reserved system module
// account), so the move changed nothing this gate demands of x/aez. In
// particular the module-account prohibition (I-10/I-11) applies verbatim to
// system modules: the promotion bought x/aez no custody relief, and the
// system-side check below is what now enforces that.
//
// What the gate has NEVER checked, and still does not, is whether a module
// registers a Msg service or a Begin/EndBlocker. That distinction lives in
// app/aetra_core_wiring_test.go, which asserts the absence of Begin/EndBlockers
// for the PROTOTYPE family only. So "x/aez is inert" was never a property this
// gate enforced -- it was a property of x/aez's membership in that family, and
// Phase 2 ends it by moving x/aez out. The routing execution point stays
// RoutingExecutionPointAnteAdmissionOnly and is still hard-rejected otherwise:
// x/aez's table is now governable, but NOTHING routes on it.
//
// The honest statement of the post-Phase-2 gate: it guarantees the declared
// module sets are wired consistently and hold no custody. It does not, and never
// did, guarantee that any module is dormant.
func (app *L1App) ValidateAetraCoreWiringGate() error {
	if app == nil || app.ModuleManager == nil {
		return fmt.Errorf("aether core wiring gate requires initialized app")
	}
	if AetraCoreRoutingExecutionPoint() != RoutingExecutionPointAnteAdmissionOnly {
		return fmt.Errorf("unsupported routing execution point %s", AetraCoreRoutingExecutionPoint())
	}
	prototypeModuleNames := AetraCorePrototypeModuleNames()
	prototypeStoreKeys := AetraCorePrototypeStoreKeys()
	if len(prototypeModuleNames) != len(prototypeStoreKeys) {
		return fmt.Errorf("prototype module/store key count mismatch")
	}
	moduleAccountPermissions := GetMaccPerms()
	for i, moduleName := range prototypeModuleNames {
		if _, found := app.ModuleManager.Modules[moduleName]; !found {
			return fmt.Errorf("prototype module %s is not registered", moduleName)
		}
		storeKey := prototypeStoreKeys[i]
		if _, found := app.keys[storeKey]; !found {
			return fmt.Errorf("prototype module %s store key %s is not mounted", moduleName, storeKey)
		}
		if _, found := moduleAccountPermissions[moduleName]; found && !IsReservedSystemModuleAccountName(moduleName) {
			return fmt.Errorf("prototype module %s must not have module account permissions", moduleName)
		}
		// AEZ Phase 2 addition. "A prototype module has no block-lifecycle
		// hook" was previously asserted only by
		// aetra_core_wiring_test.go, so a prototype module could grow a
		// BeginBlocker and reach a production binary with nothing but a
		// unit test standing in the way. It is the defining property of
		// the family and it belongs where the family is declared.
		//
		// Phase 2 is exactly the case it guards. x/aez needed a
		// BeginBlocker to swap the routing table; with this check, adding
		// one WITHOUT also promoting x/aez out of prototypeModules is a
		// startup panic on every node, not a red test someone can skip.
		// That is what makes "the promotion and the BeginBlocker are one
		// change" a structural fact rather than a review convention.
		if _, found := app.ModuleManager.Modules[moduleName].(appmodule.HasBeginBlocker); found {
			return fmt.Errorf("prototype module %s must not implement BeginBlocker; promote it to systemModules first", moduleName)
		}
		if _, found := app.ModuleManager.Modules[moduleName].(appmodule.HasEndBlocker); found {
			return fmt.Errorf("prototype module %s must not implement EndBlocker; promote it to systemModules first", moduleName)
		}
	}
	systemModuleNames := AetraCoreSystemModuleNames()
	systemStoreKeys := AetraCoreSystemStoreKeys()
	if len(systemModuleNames) != len(systemStoreKeys) {
		return fmt.Errorf("system module/store key count mismatch")
	}
	for i, moduleName := range systemModuleNames {
		if _, found := app.ModuleManager.Modules[moduleName]; !found {
			return fmt.Errorf("system module %s is not registered", moduleName)
		}
		storeKey := systemStoreKeys[i]
		if _, found := app.keys[storeKey]; !found {
			return fmt.Errorf("system module %s store key %s is not mounted", moduleName, storeKey)
		}
		if _, found := moduleAccountPermissions[moduleName]; found && !IsReservedSystemModuleAccountName(moduleName) {
			return fmt.Errorf("system module %s must not have module account permissions", moduleName)
		}
	}
	return nil
}
