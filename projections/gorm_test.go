package projections_test

import (
	"encoding/json"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/wepala/weos/controllers/rest"
	"github.com/wepala/weos/model"
	"github.com/wepala/weos/projections"
	"golang.org/x/net/context"
	"testing"
)

func TestGORMDB_GORMModel(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error setting up api: %s", err)
	}
	t.Run("setup blog model", func(t *testing.T) {
		contentType1 := "Blog"
		p1 := map[string]interface{}{"title": "test", "description": "Lorem Ipsum", "url": "https://wepala.com", "created": "2006-01-02T15:04:00Z"}
		payload1, err := json.Marshal(p1)
		if err != nil {
			t.Errorf("unexpected error marshalling entity; %s", err)
		}
		schemas := rest.CreateSchema(context.Background(), api.EchoInstance(), api.Swagger)
		entityFactory1 := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType1, api.Swagger.Components.Schemas[contentType1].Value, schemas[contentType1])
		projection, err := projections.NewProjection(context.Background(), gormDB, api.EchoInstance().Logger)
		if err != nil {
			t.Fatal(err)
		}
		//check the builder returned to ensure it has what is expected
		gormModel, err := projection.GORMModel(entityFactory1.Name(), entityFactory1.Schema(), payload1)
		if err != nil {
			t.Fatalf("unexpected error creating builder '%s'", err)
		}
		json.Unmarshal(payload1, &gormModel)
		json.Unmarshal([]byte(`{
						"table_alias": "`+`Blog`+`"
					}`), &gormModel)
		json.Unmarshal([]byte(`{"posts":[{
						"table_alias": "`+`Post`+`"
					}]}`), &gormModel)
		reader := ds.NewReader(gormModel)

		gormModel2 := entityFactory1.Builder(context.Background()).Build().New()
		json.Unmarshal(payload1, &gormModel2)
		json.Unmarshal([]byte(`{
						"table_alias": "`+`Blog`+`"
					}`), &gormModel2)
		json.Unmarshal([]byte(`{"posts":[{
						"table_alias": "`+`Post`+`"
					}]}`), &gormModel2)

		reader2 := ds.NewReader(gormModel2)

		subreaders := ds.NewReader(reader.GetField("Posts").Interface())
		subreader := subreaders.ToSliceOfReaders()[0]
		if !subreader.HasField("Table") {
			t.Fatalf("expected the sub model post to have a field Table")
		}

		if subreader.GetField("Table").String() != "Post" {
			t.Errorf("expected the table for the post model to be '%s', got '%s'", "Post", subreader.GetField("Table").String())
		}

		subreaders2 := ds.NewReader(reader2.GetField("Posts").Interface())
		subreader2 := subreaders2.ToSliceOfReaders()[0]
		if !subreader2.HasField("Table") {
			t.Fatalf("expected the post model to have a table field")
		}
		//check if the table property is set on the main entity
		if !reader.HasField("Table") {
			t.Fatalf("expected the main model to have a table field")
		}

		if !reader2.HasField("Table") {
			t.Fatalf("expected the main model to have a table field")
		}

		if reader.GetField("Table").String() != "Blog" {
			t.Errorf("expected the table for the main model to be '%s', got '%s'", "Blog", reader.GetField("Table").String())
		}

		if !reader.HasField("Posts") {
			t.Fatalf("expected model to have field '%s'", "Posts")
		}
		//run migrations and confirm that the model can be created
		err = projection.DB().Debug().AutoMigrate(gormModel)
		if err != nil {
			t.Fatalf("error running auto migration '%s'", err)
		}

		//check that the expected tables have been created
		if !projection.DB().Migrator().HasTable("Blog") {
			t.Fatalf("expected the blog table to be created")
		}
		//check Post is created
		if !projection.DB().Migrator().HasTable("Post") {
			t.Fatalf("expected the post table to be created")
		}

	})
	t.Run("setup post model", func(t *testing.T) {
		contentType1 := "Post"
		p1 := map[string]interface{}{"title": "test", "description": "Lorem Ipsum", "url": "https://wepala.com", "created": "2006-01-02T15:04:00Z"}
		payload1, err := json.Marshal(p1)
		if err != nil {
			t.Errorf("unexpected error marshalling entity; %s", err)
		}
		entityFactory1 := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType1, api.Swagger.Components.Schemas[contentType1].Value, nil)
		projection, err := projections.NewProjection(context.Background(), gormDB, api.EchoInstance().Logger)
		if err != nil {
			t.Fatal(err)
		}
		//check the builder returned to ensure it has what is expected
		gormModel, err := projection.GORMModel(entityFactory1.Name(), entityFactory1.Schema(), payload1)
		if err != nil {
			t.Fatalf("unexpected error creating builder '%s'", err)
		}

		reader := ds.NewReader(gormModel)

		//check if the table property is set on the main entity
		if !reader.HasField("Table") {
			t.Fatalf("expected the main model to have a table field")
		}

		if reader.GetField("Table").String() != "Post" {
			t.Errorf("expected the table for the main model to be '%s', got '%s'", "Post", reader.GetField("Table").String())
		}

		//run migrations and confirm that the model can be created
		err = projection.DB().Debug().AutoMigrate(gormModel)
		if err != nil {
			t.Fatalf("error running auto migration '%s'", err)
		}
		//check Post is created
		if !projection.DB().Migrator().HasTable("Post") {
			t.Fatalf("expected the post table to be created")
		}

	})
	t.Run("setup post model then blog model", func(t *testing.T) {
		contentType1 := "Post"
		p1 := map[string]interface{}{"title": "test", "description": "Lorem Ipsum", "url": "https://wepala.com", "created": "2006-01-02T15:04:00Z"}
		payload1, err := json.Marshal(p1)
		if err != nil {
			t.Errorf("unexpected error marshalling entity; %s", err)
		}
		schemas := rest.CreateSchema(context.Background(), api.EchoInstance(), api.Swagger)
		entityFactory1 := new(model.DefaultEntityFactory).FromSchemaAndBuilder(contentType1, api.Swagger.Components.Schemas[contentType1].Value, schemas["Post"])
		entityFactory2 := new(model.DefaultEntityFactory).FromSchemaAndBuilder("Blog", api.Swagger.Components.Schemas["Blog"].Value, schemas["Blog"])
		entityFactory3 := new(model.DefaultEntityFactory).FromSchemaAndBuilder("Author", api.Swagger.Components.Schemas["Author"].Value, schemas["Author"])
		entityFactory4 := new(model.DefaultEntityFactory).FromSchemaAndBuilder("Category", api.Swagger.Components.Schemas["Category"].Value, schemas["Category"])
		projection, err := projections.NewProjection(context.Background(), gormDB, api.EchoInstance().Logger)
		if err != nil {
			t.Fatal(err)
		}
		//check the builder returned to ensure it has what is expected
		gormModel, err := projection.GORMModel(entityFactory1.Name(), entityFactory1.Schema(), payload1)
		if err != nil {
			t.Fatalf("unexpected error creating builder '%s'", err)
		}

		gormModel2, err := projection.GORMModel(entityFactory2.Name(), entityFactory2.Schema(), payload1)
		if err != nil {
			t.Fatalf("unexpected error creating builder '%s'", err)
		}

		gormModel3, err := projection.GORMModel(entityFactory3.Name(), entityFactory3.Schema(), payload1)
		if err != nil {
			t.Fatalf("unexpected error creating builder '%s'", err)
		}

		gormModel4, err := projection.GORMModel(entityFactory4.Name(), entityFactory4.Schema(), payload1)
		if err != nil {
			t.Fatalf("unexpected error creating builder '%s'", err)
		}

		//run migrations and confirm that the model can be created
		err = projection.DB().Debug().AutoMigrate(gormModel3, gormModel4, gormModel, gormModel2)
		if err != nil {
			t.Fatalf("error running auto migration '%s'", err)
		}
		//check Post is created
		if !projection.DB().Migrator().HasTable("Post") {
			t.Fatalf("expected the post table to be created")
		}

		//check Blog is created
		if !projection.DB().Migrator().HasTable("Blog") {
			t.Fatalf("expected the blog table to be created")
		}

		//there should be no tables with no name
		if projection.DB().Migrator().HasTable("") {
			t.Fatalf("there should be no tables without a name")
		}
	})
}
