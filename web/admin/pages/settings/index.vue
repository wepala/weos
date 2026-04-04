<!--
  Copyright (C) 2026 Wepala, LLC

  This program is free software: you can redistribute it and/or modify
  it under the terms of the GNU Affero General Public License as published by
  the Free Software Foundation, either version 3 of the License, or
  (at your option) any later version.

  This program is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
  GNU Affero General Public License for more details.

  You should have received a copy of the GNU Affero General Public License
  along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

<template>
  <div>
    <a-page-header title="Settings" />
    <a-row :gutter="16">
      <a-col :span="8">
        <NuxtLink to="/settings/roles" style="text-decoration: none">
          <a-card hoverable title="Roles">
            <p>Manage the roles available when editing users.</p>
          </a-card>
        </NuxtLink>
      </a-col>
      <a-col :span="8">
        <NuxtLink to="/settings/role-access" style="text-decoration: none">
          <a-card hoverable title="Role Access">
            <p>Configure which resource types each role can access.</p>
          </a-card>
        </NuxtLink>
      </a-col>
      <a-col :span="8">
        <NuxtLink to="/settings/menus" style="text-decoration: none">
          <a-card hoverable title="Sidebar Menu">
            <p>Choose which resource types appear in the sidebar navigation.</p>
          </a-card>
        </NuxtLink>
      </a-col>
    </a-row>
  </div>
</template>
