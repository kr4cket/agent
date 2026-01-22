package domvalidation

const domAnalysisScriptTmpl = `(() => {
	const x = %d;
	const y = %d;
	const element = document.elementFromPoint(x, y);
	if (!element) {
		return { found: false, reason: "No element at coordinates (" + x + ", " + y + ")" };
	}
	let candidates = [];
	let current = element;
	let depth = 0;
	const maxDepth = 5;
	while (current && current !== document.body && depth < maxDepth) {
		const tagName = current.tagName.toLowerCase();
		const role = current.getAttribute('role') || '';
		const rect = current.getBoundingClientRect();
		const clickTolerance = 50;
		const inClickArea = (
			rect.left - clickTolerance <= x && x <= rect.right + clickTolerance &&
			rect.top - clickTolerance <= y && y <= rect.bottom + clickTolerance
		);
		if (inClickArea) {
			let actualRole = role;
			if (!actualRole) {
				if (tagName === 'button') actualRole = 'button';
				else if (tagName === 'a') actualRole = 'link';
				else if (tagName === 'input') {
					const inputType = current.type || '';
					if (['text','search','','email','tel','url'].includes(inputType)) actualRole = 'textbox';
					else if (['button','submit'].includes(inputType)) actualRole = 'button';
					else actualRole = 'generic';
				} else if (tagName === 'textarea') actualRole = 'textbox';
				else if (tagName === 'select') actualRole = 'combobox';
				else if (current.contentEditable === 'true' || current.isContentEditable) actualRole = 'textbox';
				else actualRole = 'generic';
			}
			if (role === 'textbox') actualRole = 'textbox';
			const placeholder = current.getAttribute('placeholder') || '';
			const text = (current.textContent || current.value || '').substring(0, 100);
			const name = current.getAttribute('name') || current.getAttribute('aria-label') || current.getAttribute('title') || '';
			const isVisible = current.offsetWidth > 0 && current.offsetHeight > 0;
			const isDisabled = current.disabled || current.getAttribute('disabled') !== null;
			const isClickable = tagName === 'button' || tagName === 'a' || tagName === 'input' ||
				['button','link','textbox'].includes(role) || current.onclick || current.style.cursor === 'pointer' ||
				window.getComputedStyle(current).cursor === 'pointer';
			candidates.push({
				element: current, role: actualRole, tagName, placeholder, text, name,
				isVisible, isDisabled, isClickable, depth,
				rect: { left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom, width: rect.width, height: rect.height }
			});
		}
		current = current.parentElement;
		depth++;
	}
	if (candidates.length === 0) {
		return { found: false, reason: "No clickable elements found in area around coordinates (" + x + ", " + y + ")" };
	}
	let bestCandidate = null;
	for (const candidate of candidates) {
		if (!candidate.isVisible || candidate.isDisabled) continue;
		if (!bestCandidate) { bestCandidate = candidate; continue; }
		if (candidate.isClickable && !bestCandidate.isClickable) { bestCandidate = candidate; continue; }
		if (candidate.depth < bestCandidate.depth) bestCandidate = candidate;
	}
	if (!bestCandidate) {
		for (const candidate of candidates) {
			if (candidate.isVisible) { bestCandidate = candidate; break; }
		}
	}
	if (!bestCandidate) {
		return { found: false, reason: "No suitable element found at coordinates (" + x + ", " + y + ")" };
	}
	return {
		found: true, role: bestCandidate.role, placeholder: bestCandidate.placeholder,
		text: bestCandidate.text, name: bestCandidate.name, tagName: bestCandidate.tagName,
		isVisible: bestCandidate.isVisible, isDisabled: bestCandidate.isDisabled, isClickable: bestCandidate.isClickable,
		depth: bestCandidate.depth, candidatesCount: candidates.length,
		elementTag: element.tagName.toLowerCase()
	};
})()`
