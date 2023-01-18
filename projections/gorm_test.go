package projections_test

import (
	"encoding/json"
	ds "github.com/ompluscator/dynamic-struct"
	"github.com/segmentio/ksuid"
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
		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
		}
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

		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
		}

	})
	t.Run("setup post model", func(t *testing.T) {
		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
		}

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

		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
		}

	})
	t.Run("setup post model then blog model", func(t *testing.T) {
		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
		}

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

		err = gormDB.Migrator().DropTable("blog_posts")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "blog_posts", err)
		}
		err = gormDB.Migrator().DropTable("Blog")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Blog", err)
		}
		err = gormDB.Migrator().DropTable("Post")
		if err != nil {
			t.Errorf("error removing table '%s' '%s'", "Post", err)
		}
	})
	t.Run("setup model with reference to inline schemas", func(t *testing.T) {
		api, err := rest.New("./fixtures/complex-spec.yaml")
		if err != nil {
			t.Fatalf("unexpected error setting up api: %s", err)
		}
		contentType1 := "Patient"
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
						"table_alias": "`+`Patient`+`"
					}`), &gormModel)
		reader := ds.NewReader(gormModel)

		//check if the table property is set on the main entity
		if !reader.HasField("Table") {
			t.Fatalf("expected the main model to have a table field")
		}

		if reader.GetField("Table").String() != "Patient" {
			t.Errorf("expected the table for the main model to be '%s', got '%s'", "Blog", reader.GetField("Table").String())
		}

		//run migrations and confirm that the model can be created
		err = projection.DB().Debug().AutoMigrate(gormModel)
		if err != nil {
			t.Fatalf("error running auto migration '%s'", err)
		}

		//check that the expected tables have been created
		if !projection.DB().Migrator().HasTable("Patient") {
			t.Fatalf("expected the blog table to be created")
		}
		//check the no table Identifier is created
		if projection.DB().Migrator().HasTable("Identifier") {
			t.Fatalf("expected the identifier table to not be created")
		}

		defer gormDB.Migrator().DropTable("Patient")
	})
}

func TestGORMDB_GORMModels(t *testing.T) {
	//load open api spec
	api, err := rest.New("./fixtures/complex-spec.yaml")
	if err != nil {
		t.Fatalf("unexpected error setting up api: %s", err)
	}
	projection, err := projections.NewProjection(context.Background(), gormDB, api.EchoInstance().Logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("convert inline arrays", func(t *testing.T) {
		payload := make(map[string]interface{})
		payload["contact"] = []struct {
			Name         string `json:"name"`
			RelationShip struct {
				Text string
			} `json:"relationShip"`
		}{
			{
				Name: "Medgar Evans",
			},
		}

		payloadData, err := json.Marshal(&payload)
		if err != nil {
			t.Fatalf("unexpected error setting up payload '%s'", err)
		}

		model, err := projection.GORMModel("Patient", api.GetConfig().Components.Schemas["Patient"].Value, payloadData)
		if err != nil {
			t.Fatalf("unexpected error setting up model '%s'", err)
		}

		reader := ds.NewReader(model)
		if !reader.HasField("Contact") {
			t.Fatalf("expected contact field to exist")
		}

		contactString := reader.GetField("Contact").String()
		if contactString == "" {
			t.Fatalf("expected contact to be json string")
		}

		var actualContacts []map[string]interface{}
		err = json.Unmarshal([]byte(contactString), &actualContacts)
		if err != nil {
			t.Fatalf("error unmarshiling conact '%s'", err)
		}

		if actualContacts[0]["name"] != "Medgar Evans" {
			t.Errorf("expected contact name to be '%s', got '%s'", "Medgar Evans", actualContacts[0]["name"])
		}

	})

	t.Run("convert array item schema marked inline to inline string", func(t *testing.T) {
		payload := make(map[string]interface{})
		payload["identifier"] = []struct {
			Use    string `json:"use"`
			System string `json:"system"`
			Value  string `json:"value"`
			Type   struct {
				Coding []struct {
					System  string `json:"system"`
					Code    string `json:"code"`
					Display string `json:"display"`
				} `json:"coding"`
				Text string `json:"text"`
			} `json:"type"`
		}{
			{
				Use:    "official",
				System: "test",
				Value:  "someid",
				Type: struct {
					Coding []struct {
						System  string `json:"system"`
						Code    string `json:"code"`
						Display string `json:"display"`
					} `json:"coding"`
					Text string `json:"text"`
				}{
					Coding: []struct {
						System  string `json:"system"`
						Code    string `json:"code"`
						Display string `json:"display"`
					}{
						{
							System: "http://terminology.hl7.org/CodeSystem/v2-0203",
							Code:   "MR",
						},
					},
					Text: "display",
				},
			},
		}

		payloadData, err := json.Marshal(&payload)
		if err != nil {
			t.Fatalf("unexpected error setting up payload '%s'", err)
		}

		model, err := projection.GORMModel("Patient", api.GetConfig().Components.Schemas["Patient"].Value, payloadData)
		if err != nil {
			t.Fatalf("unexpected error setting up model '%s'", err)
		}

		reader := ds.NewReader(model)
		if !reader.HasField("Identifier") {
			t.Fatalf("expected contact field to exist")
		}

		identifierString := reader.GetField("Identifier").String()
		if identifierString == "" {
			t.Fatalf("expected contact to be json string")
		}

		var actualIdentifiers []map[string]interface{}
		err = json.Unmarshal([]byte(identifierString), &actualIdentifiers)
		if err != nil {
			t.Fatalf("error unmarshiling conact '%s'", err)
		}

		if actualIdentifiers[0]["value"] != "someid" {
			t.Errorf("expected identifier value to be '%s', got '%s'", "someid", actualIdentifiers[0]["value"])
		}

	})
	t.Run("convert object schema marked inline to inline string", func(t *testing.T) {
		payload := make(map[string]interface{})
		payload["maritalStatus"] = []struct {
			Use    string `json:"use"`
			System string `json:"system"`
			Value  string `json:"value"`
		}{
			{
				Use:    "official",
				System: "test",
				Value:  "someid",
			},
		}

		payloadData, err := json.Marshal(&payload)
		if err != nil {
			t.Fatalf("unexpected error setting up payload '%s'", err)
		}

		model, err := projection.GORMModel("Patient", api.GetConfig().Components.Schemas["Patient"].Value, payloadData)
		if err != nil {
			t.Fatalf("unexpected error setting up model '%s'", err)
		}

		reader := ds.NewReader(model)
		if !reader.HasField("MaritalStatus") {
			t.Fatalf("expected MaritalStatus field to exist")
		}

		maritalStatusString := reader.GetField("MaritalStatus").String()
		if maritalStatusString == "" {
			t.Fatalf("expected contact to be json string")
		}

		var actualIdentifiers []map[string]interface{}
		err = json.Unmarshal([]byte(maritalStatusString), &actualIdentifiers)
		if err != nil {
			t.Fatalf("error unmarshiling conact '%s'", err)
		}

		if actualIdentifiers[0]["value"] != "someid" {
			t.Errorf("expected identifier value to be '%s', got '%s'", "someid", actualIdentifiers[0]["value"])
		}

	})
}

type TestBlog struct {
	model.AggregateRoot
	Title string `gorm:"not null"`
}

func TestGORMDB_Persist(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error setting up api: %s", err)
	}
	projection, err := projections.NewProjection(context.Background(), gormDB, api.EchoInstance().Logger)
	if err != nil {
		t.Fatal(err)
	}
	projection.Migrate(context.TODO(), api.GetConfig())
	t.Run("create a content entity", func(t *testing.T) {
		api.RegisterEntityFactory("Blog", new(model.DefaultEntityFactory).FromSchema("Blog", api.GetConfig().Components.Schemas["Blog"].Value))
		ef, err := api.GetEntityFactory("Blog")
		if err != nil {
			t.Fatalf("unexpected error getting entity factory '%s'", err)
		}
		blog := `{"title":"My Blog","description":"This is my blog"}`
		contentEntity, err := ef.CreateEntityWithValues(context.Background(), []byte(blog))
		if err != nil {
			t.Fatalf("unexpected error creating entity '%s'", err)
		}
		err = projection.Persist([]model.Entity{contentEntity})
		if err != nil {
			t.Fatalf("unexpected error persisting entity '%s'", err)
		}
		//check Blog is created
		tmodel, err := projection.GORMModel("Blog", ef.Schema(), []byte(`{}`))
		if err != nil {
			t.Fatalf("unexpected error creating model '%s'", err)
		}
		projection.DB().Table(contentEntity.Name).Find(&tmodel, "title = ?", "My Blog")
		reader := ds.NewReader(tmodel)
		if reader.GetField("Title").String() != "My Blog" {
			t.Errorf("expected title to be '%s', got '%s'", "My Blog", reader.GetField("Title").String())
		}
	})

	t.Run("create a regular model", func(t *testing.T) {
		blog := TestBlog{Title: "My Blog"}
		blog.ID = ksuid.New().String()
		err := projection.DB().AutoMigrate(&TestBlog{})
		if err != nil {
			t.Fatalf("unexpected error creating model '%s'", err)
		}
		err = projection.Persist([]model.Entity{&blog})
		if err != nil {
			t.Fatalf("unexpected error persisting entity '%s'", err)
		}
		//check Blog is created
		tresult := &TestBlog{}
		result := projection.DB().Debug().Find(&tresult, "title = ?", "My Blog")
		if result.Error != nil {
			t.Errorf("unexpected error finding entity '%s'", result.Error)
		}
		if tresult.Title != "My Blog" {
			t.Errorf("expected title to be '%s', got '%s'", "My Blog", tresult.Title)
		}
	})

	err = gormDB.Migrator().DropTable("Blog")
	if err != nil {
		t.Errorf("error removing table '%s' '%s'", "Post", err)
	}

	err = gormDB.Migrator().DropTable("test_blogs")
	if err != nil {
		t.Errorf("error removing table '%s' '%s'", "Post", err)
	}
}

func TestGORMDB_Remove(t *testing.T) {
	//load open api spec
	api, err := rest.New("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error setting up api: %s", err)
	}
	projection, err := projections.NewProjection(context.Background(), gormDB, api.EchoInstance().Logger)
	if err != nil {
		t.Fatal(err)
	}
	projection.Migrate(context.TODO(), api.GetConfig())
	t.Run("remove a content entity", func(t *testing.T) {
		api.RegisterEntityFactory("Blog", new(model.DefaultEntityFactory).FromSchema("Blog", api.GetConfig().Components.Schemas["Blog"].Value))
		ef, err := api.GetEntityFactory("Blog")
		if err != nil {
			t.Fatalf("unexpected error getting entity factory '%s'", err)
		}
		blog := `{"title":"My Blog","description":"This is my blog"}`
		contentEntity, err := ef.CreateEntityWithValues(context.Background(), []byte(blog))
		if err != nil {
			t.Fatalf("unexpected error creating entity '%s'", err)
		}
		err = projection.Persist([]model.Entity{contentEntity})
		if err != nil {
			t.Fatalf("unexpected error persisting entity '%s'", err)
		}
		//check Blog is created
		tmodel, err := projection.GORMModel("Blog", ef.Schema(), []byte(`{}`))
		if err != nil {
			t.Fatalf("unexpected error creating model '%s'", err)
		}
		projection.DB().Table(contentEntity.Name).Find(&tmodel, "title = ?", "My Blog")
		reader := ds.NewReader(tmodel)
		if reader.GetField("Title").String() != "My Blog" {
			t.Errorf("expected title to be '%s', got '%s'", "My Blog", reader.GetField("Title").String())
		}
		err = projection.Remove([]model.Entity{contentEntity})
		if err != nil {
			t.Fatalf("unexpected error removing entity '%s'", err)
		}
		result := projection.DB().Debug().Find(&tmodel, "title = ?", "My Blog")
		if result.RowsAffected != 0 {
			t.Errorf("expected rows affected to be '%d', got '%d'", 0, result.RowsAffected)
		}
	})

	t.Run("remove a regular model", func(t *testing.T) {
		blog := TestBlog{Title: "My Blog"}
		blog.ID = ksuid.New().String()
		err := projection.DB().AutoMigrate(&TestBlog{})
		if err != nil {
			t.Fatalf("unexpected error creating model '%s'", err)
		}
		err = projection.Persist([]model.Entity{&blog})
		if err != nil {
			t.Fatalf("unexpected error persisting entity '%s'", err)
		}
		//check Blog is created
		tresult := &TestBlog{}
		result := projection.DB().Debug().Find(&tresult, "title = ?", "My Blog")
		if result.Error != nil {
			t.Errorf("unexpected error finding entity '%s'", result.Error)
		}
		if tresult.Title != "My Blog" {
			t.Errorf("expected title to be '%s', got '%s'", "My Blog", tresult.Title)
		}

		err = projection.Remove([]model.Entity{tresult})
		if err != nil {
			t.Fatalf("unexpected error removing entity '%s'", err)
		}
		result = projection.DB().Debug().Find(&tresult, "title = ?", "My Blog")
		if result.RowsAffected != 0 {
			t.Errorf("expected rows affected to be '%d', got '%d'", 0, result.RowsAffected)
		}
	})
}

func TestGORMDB_GetByKey(t *testing.T) {
	err := gormDB.Migrator().DropTable("Blog")
	if err != nil {
		t.Errorf("error removing table '%s' '%s'", "Post", err)
	}

	defer gormDB.Migrator().DropTable("Blog")

	api, err := rest.New("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error setting up api: %s", err)
	}
	projection, err := projections.NewProjection(context.Background(), gormDB, api.EchoInstance().Logger)
	if err != nil {
		t.Fatal(err)
	}
	projection.Migrate(context.TODO(), api.GetConfig())

	t.Run("get entity with associations", func(t *testing.T) {
		api.RegisterEntityFactory("Blog", new(model.DefaultEntityFactory).FromSchema("Blog", api.GetConfig().Components.Schemas["Blog"].Value))
		ef, err := api.GetEntityFactory("Blog")
		if err != nil {
			t.Fatalf("unexpected error getting entity factory '%s'", err)
		}
		blog := `{"title":"My Blog","url":"http://wepala.com","description":"This is my blog","author":{"id":"test","email":"test.example.org", "firstName":"John", "lastName":"Doe"}}`
		contentEntity, err := ef.CreateEntityWithValues(context.Background(), []byte(blog))
		if err != nil {
			t.Fatalf("unexpected error creating entity '%s'", err)
		}
		err = projection.Persist([]model.Entity{contentEntity})
		if err != nil {
			t.Fatalf("unexpected error persisting entity '%s'", err)
		}
		//check Blog is created
		tmodel, err := projection.GORMModel("Blog", ef.Schema(), []byte(`{}`))
		if err != nil {
			t.Fatalf("unexpected error creating model '%s'", err)
		}
		projection.DB().Table(contentEntity.Name).Find(&tmodel, "title = ?", "My Blog")
		reader := ds.NewReader(tmodel)
		if !reader.HasField("Id") {
			t.Fatalf("expected id to be present")
		}
		//use the generated id to look up the blog by key
		tblog, err := projection.GetByKey(context.TODO(), ef, map[string]interface{}{
			"id": reader.GetField("Id").Uint(),
		})
		if err != nil {
			t.Fatalf("unexpected error getting entity '%s'", err)
		}
		if tblog == nil {
			t.Fatalf("expected entity to be found")
		}
		if author := tblog.GetInterface("author"); author != nil {
			var authorMap map[string]interface{}
			authorRaw, err := json.Marshal(author)
			if err != nil {
				t.Errorf("unexpected error marshalling author '%s'", err)
			}
			err = json.Unmarshal(authorRaw, &authorMap)
			if authorMap["firstName"] != "John" {
				t.Errorf("expected author to be '%s', got '%s'", "John", authorMap["firstName"])
			}
		} else {
			t.Errorf("expected author to be '%s', got '%T'", "John Doe", tblog.GetInterface("author"))
		}

	})

}

func TestGORMDB_GetContentEntity(t *testing.T) {
	err := gormDB.Migrator().DropTable("Blog")
	if err != nil {
		t.Errorf("error removing table '%s' '%s'", "Post", err)
	}

	defer gormDB.Migrator().DropTable("Blog")

	api, err := rest.New("../controllers/rest/fixtures/blog.yaml")
	if err != nil {
		t.Fatalf("unexpected error setting up api: %s", err)
	}
	projection, err := projections.NewProjection(context.Background(), gormDB, api.EchoInstance().Logger)
	if err != nil {
		t.Fatal(err)
	}
	projection.Migrate(context.TODO(), api.GetConfig())

	t.Run("get entity with associations", func(t *testing.T) {
		api.RegisterEntityFactory("Blog", new(model.DefaultEntityFactory).FromSchema("Blog", api.GetConfig().Components.Schemas["Blog"].Value))
		ef, err := api.GetEntityFactory("Blog")
		if err != nil {
			t.Fatalf("unexpected error getting entity factory '%s'", err)
		}
		blog := `{"title":"My Blog","url":"http://wepala.com","description":"This is my blog","author":{"id":"test","email":"test.example.org", "firstName":"John", "lastName":"Doe"}}`
		contentEntity, err := ef.CreateEntityWithValues(context.Background(), []byte(blog))
		if err != nil {
			t.Fatalf("unexpected error creating entity '%s'", err)
		}
		err = projection.Persist([]model.Entity{contentEntity})
		if err != nil {
			t.Fatalf("unexpected error persisting entity '%s'", err)
		}
		//check Blog is created
		tmodel, err := projection.GORMModel("Blog", ef.Schema(), []byte(`{}`))
		if err != nil {
			t.Fatalf("unexpected error creating model '%s'", err)
		}
		projection.DB().Table(contentEntity.Name).Find(&tmodel, "title = ?", "My Blog")
		reader := ds.NewReader(tmodel)
		if !reader.HasField("WeosID") {
			t.Fatalf("expected Weos_id to be present")
		}
		//use the generated id to look up the blog by key
		tblog, err := projection.GetContentEntity(context.TODO(), ef, reader.GetField("WeosID").String())
		if err != nil {
			t.Fatalf("unexpected error getting entity '%s'", err)
		}
		if tblog == nil {
			t.Fatalf("expected entity to be found")
		}
		if author := tblog.GetInterface("author"); author != nil {
			var authorMap map[string]interface{}
			authorRaw, err := json.Marshal(author)
			if err != nil {
				t.Errorf("unexpected error marshalling author '%s'", err)
			}
			err = json.Unmarshal(authorRaw, &authorMap)
			if authorMap["firstName"] != "John" {
				t.Errorf("expected author to be '%s', got '%s'", "John", authorMap["firstName"])
			}
		} else {
			t.Errorf("expected author to be '%s', got '%T'", "John Doe", tblog.GetInterface("author"))
		}

	})
}
