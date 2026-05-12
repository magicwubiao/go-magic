import { isBrowser } from "./is-browser.mjs";
export const isImageSupportNativeLazy = isBrowser && 'loading' in document.createElement('img');