import { isBrowser } from "./is-browser.mjs";
export const isChrome = isBrowser && 'chrome' in window;
export const isFirefox = isBrowser && navigator.userAgent.includes('Firefox');
export const isSafari = isBrowser && navigator.userAgent.includes('Safari') && !isChrome;