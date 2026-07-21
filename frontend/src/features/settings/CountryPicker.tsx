import { useState } from "react";
import { Autocomplete, Description, Heading, Label, ListBox, SearchField, Tag, TagGroup } from "@heroui/react";
import { countryFlag, countryOptions } from "./countries";

interface CountryPickerProps {
  label: string;
  value: string[];
  description: string;
  onChange: (value: string[]) => void;
}

export function CountryPicker({ label, value, description, onChange }: CountryPickerProps) {
  const [selectedKey, setSelectedKey] = useState<string | null>(value.at(-1) ?? null);
  const selected = value.map((code) => countryOptions.find((country) => country.code === code) ?? { code, name: code, searchText: code });
  return <div className="field field-wide country-field">
    <Autocomplete
      aria-label={label}
      disabledKeys={value}
      fullWidth
      value={selectedKey}
      variant="secondary"
      onChange={(next) => {
        if (next == null) return;
        const code = String(next);
        setSelectedKey(code);
        if (!value.includes(code)) onChange([...value, code]);
      }}
    >
      <Label>{label}</Label>
      <Autocomplete.Trigger aria-label={`${label}选择器`}>
        <Autocomplete.Value>{value.length === 0 ? "搜索并添加国家" : `继续添加（已选 ${value.length} 个）`}</Autocomplete.Value>
        <Autocomplete.Indicator />
      </Autocomplete.Trigger>
      <Description>{description}</Description>
      <Autocomplete.Popover>
        <Heading className="sr-only" slot="title">{label}候选</Heading>
        <Autocomplete.Filter filter={(text, input) => text.toLocaleUpperCase().includes(input.trim().toLocaleUpperCase())}>
          <SearchField aria-label={`搜索${label}`} autoFocus={false}>
            <SearchField.Group>
              <SearchField.SearchIcon />
              <SearchField.Input placeholder="搜索国家名称或代码" />
              <SearchField.ClearButton />
            </SearchField.Group>
          </SearchField>
          <ListBox aria-label={`${label}候选`} items={countryOptions}>
            {(country) => <ListBox.Item id={country.code} textValue={country.searchText}>
              <span className="country-option"><span className="country-flag" aria-hidden="true">{countryFlag(country.code)}</span><span>{country.name}</span><code>{country.code}</code></span>
              <ListBox.ItemIndicator />
            </ListBox.Item>}
          </ListBox>
        </Autocomplete.Filter>
      </Autocomplete.Popover>
    </Autocomplete>
    {selected.length > 0 && <TagGroup className="country-tags" aria-label={`${label}已选`} size="sm" variant="surface" onRemove={(keys) => {
      const removed = new Set([...keys].map(String));
      if (selectedKey != null && removed.has(selectedKey)) setSelectedKey(null);
      onChange(value.filter((code) => !removed.has(code)));
    }}>
      <TagGroup.List items={selected}>{(country) => <Tag id={country.code} textValue={country.searchText}><span className="country-tag-label"><span className="country-flag" aria-hidden="true">{countryFlag(country.code)}</span><span>{country.name}</span><code>{country.code}</code></span></Tag>}</TagGroup.List>
    </TagGroup>}
  </div>;
}
