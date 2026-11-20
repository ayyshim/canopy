# Responsive breakpoints

Defined at the top of `frontend/src/app/globals.css`. Media queries can't read CSS variables, so use the literals.

| Name | Query | Use |
|------|-------|-----|
| md | `max-width:900px` | tablets and below: side panels become bottom sheets, nav rails hide |
| sm | `max-width:640px` | phones: top bar collapses into the "..." menu |

Helper classes: `.hide-sm` / `.show-sm` (at 900px), `.hide-xs` / `.show-xs` (at 640px), `.tap` (44px target).
On touch devices (`pointer:coarse`) menu items and icon buttons are 44px tall.

Menus: use `Menu` (`components/Menu.tsx`); it closes on `pointerdown` outside so it works on touch. Menus are capped to the viewport width.
Pad edges with `env(safe-area-inset-*)`; the viewport uses `viewport-fit=cover`.
