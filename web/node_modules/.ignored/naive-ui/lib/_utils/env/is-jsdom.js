"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.isJsdom = isJsdom;
let _isJsdom;
function isJsdom() {
    if (_isJsdom === undefined) {
        _isJsdom
            = navigator.userAgent.includes('Node.js')
                || navigator.userAgent.includes('jsdom');
    }
    return _isJsdom;
}
