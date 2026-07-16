function customizeBashCliHighlighting(PrismObject) {
  const parameter = PrismObject.languages.bash?.parameter;
  if (!parameter || parameter instanceof RegExp || Array.isArray(parameter)) {
    return;
  }

  parameter.pattern = /(^|\s)-{1,2}(?:\w+:[+-]?)?[\w-]+(?:\.\w+)*(?=[=\s]|$)/;
}

module.exports = {customizeBashCliHighlighting};
