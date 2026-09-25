# Device Product Images

Device card product images are organized by source.

## Jeedom Ajax Plugin

Path: `jeedom/`

The card resolver in `src/utils/assets.ts` uses these images for device cards.

These images come from an installed official Jeedom Ajax Systems plugin. Its URL pattern is:

`https://<jeedom-host>/plugins/ajaxSystem/core/config/devices/<file>.png`

`Button_*` and `DoubleButton_*` are official transparent Ajax catalog images because the Jeedom image set does not include those models:

- `https://www.ajax-systems.uz/wp-content/themes/ajax/assets/images/template/catalog/products/Button_<color>@1x.png`
- `https://www.ajax-systems.uz/wp-content/themes/ajax/assets/images/template/catalog/products/doublebutton_<color>@1x.png`

## Dahua Cameras

Path: `dahua/`

The camera card resolver uses these product images for DahuaBridge devices. Prefer official Dahua material images when available; vendor product images are kept only for models that are not available from the Dahua material CDN.

The root `devices/` directory intentionally contains no product image files. Keep source-specific image sets in subdirectories so unused assets are easy to audit.
