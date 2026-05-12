let _isJsdom;
export function isJsdom() {
  if (_isJsdom === undefined) {
    _isJsdom = navigator.userAgent.includes('Node.js') || navigator.userAgent.includes('jsdom');
  }
  return _isJsdom;
}